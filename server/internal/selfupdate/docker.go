package selfupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// docker is the little of the Docker Engine API an update takes, spoken
// over the daemon's socket.
type docker struct {
	http *http.Client
}

func newDocker(socket string) *docker {
	return &docker{http: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
	}}}
}

type obj = map[string]any

func (d *docker) call(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker/v1.41"+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := d.http.Do(req)
	if err != nil {
		return fmt.Errorf("docker is not reachable: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		var e struct {
			Message string `json:"message"`
		}
		json.Unmarshal(raw, &e)
		if e.Message == "" {
			e.Message = res.Status
		}
		return &dockerError{status: res.StatusCode, message: e.Message}
	}
	// A pull answers 200 and reports its failure in the stream.
	if strings.HasPrefix(path, "/images/create") {
		for _, line := range bytes.Split(raw, []byte("\n")) {
			var e struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(line, &e) == nil && e.Error != "" {
				return errors.New(e.Error)
			}
		}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

type dockerError struct {
	status  int
	message string
}

func (e *dockerError) Error() string { return e.message }

func missing(err error) bool {
	var de *dockerError
	return errors.As(err, &de) && de.status == http.StatusNotFound
}

type container struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	Image string `json:"Image"` // the image's ID
	State struct {
		Running    bool `json:"Running"`
		Restarting bool `json:"Restarting"`
		Health     *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	RestartCount    int `json:"RestartCount"`
	Config          obj `json:"Config"`
	HostConfig      obj `json:"HostConfig"`
	NetworkSettings struct {
		Networks map[string]obj `json:"Networks"`
	} `json:"NetworkSettings"`
}

func (c container) name() string  { return strings.TrimPrefix(c.Name, "/") }
func (c container) image() string { s, _ := c.Config["Image"].(string); return s }

func (d *docker) inspect(ctx context.Context, name string) (container, error) {
	var c container
	return c, d.call(ctx, http.MethodGet, "/containers/"+url.PathEscape(name)+"/json", nil, &c)
}

func (d *docker) imageConfig(ctx context.Context, ref string) (obj, error) {
	var img struct {
		Config obj `json:"Config"`
	}
	return img.Config, d.call(ctx, http.MethodGet, "/images/"+url.PathEscape(ref)+"/json", nil, &img)
}

// fetch brings repository:tag to this machine. An image that cannot be
// pulled but is already here (a local build) will do.
func (d *docker) fetch(ctx context.Context, repository, tag string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	q := url.Values{"fromImage": {repository}, "tag": {tag}}
	err := d.call(ctx, http.MethodPost, "/images/create?"+q.Encode(), nil, nil)
	if err == nil {
		return nil
	}
	if _, here := d.imageConfig(ctx, repository+":"+tag); here == nil {
		return nil
	}
	return fmt.Errorf("could not download %s:%s: %w", repository, tag, err)
}

func (d *docker) remove(ctx context.Context, name string) error {
	err := d.call(ctx, http.MethodDelete, "/containers/"+url.PathEscape(name)+"?force=true", nil, nil)
	if missing(err) {
		return nil
	}
	return err
}

// sameAs builds the request that creates a container like old from another
// image. What old only has because its image said so (the command, the
// environment, the health check) is left out, so that the new image's own
// settings apply; what was given when the container was made is kept.
func sameAs(old container, oldImage obj, image string) obj {
	body := obj{}
	for k, v := range old.Config {
		body[k] = v
	}
	for _, k := range []string{"Cmd", "Entrypoint", "Healthcheck", "User", "WorkingDir", "Volumes", "ExposedPorts"} {
		if fmt.Sprint(body[k]) == fmt.Sprint(oldImage[k]) {
			delete(body, k)
		}
	}
	fromImage := map[string]bool{}
	if env, ok := oldImage["Env"].([]any); ok {
		for _, e := range env {
			fromImage[fmt.Sprint(e)] = true
		}
	}
	if env, ok := body["Env"].([]any); ok {
		kept := []any{}
		for _, e := range env {
			if !fromImage[fmt.Sprint(e)] {
				kept = append(kept, e)
			}
		}
		body["Env"] = kept
	}
	if labels, ok := body["Labels"].(obj); ok {
		was, _ := oldImage["Labels"].(obj)
		kept := obj{}
		for k, v := range labels {
			if w, ok := was[k]; !ok || w != v {
				kept[k] = v
			}
		}
		body["Labels"] = kept
	}
	// A name the container got from its own ID belongs to the old one.
	if h, _ := body["Hostname"].(string); h != "" && strings.HasPrefix(old.ID, h) {
		delete(body, "Hostname")
	}
	body["Image"] = image
	body["HostConfig"] = old.HostConfig
	mode, _ := old.HostConfig["NetworkMode"].(string)
	if mode != "host" && mode != "none" && mode != "default" && mode != "bridge" && !strings.HasPrefix(mode, "container:") {
		if net, ok := old.NetworkSettings.Networks[mode]; ok {
			body["NetworkingConfig"] = obj{"EndpointsConfig": obj{mode: obj{"Aliases": net["Aliases"], "IPAMConfig": net["IPAMConfig"]}}}
		}
	}
	return body
}

func (d *docker) create(ctx context.Context, name string, body obj) error {
	return d.call(ctx, http.MethodPost, "/containers/create?name="+url.QueryEscape(name), body, nil)
}

func (d *docker) start(ctx context.Context, name string) error {
	return d.call(ctx, http.MethodPost, "/containers/"+url.PathEscape(name)+"/start", nil, nil)
}

func (d *docker) stop(ctx context.Context, name string) error {
	err := d.call(ctx, http.MethodPost, "/containers/"+url.PathEscape(name)+"/stop?t=30", nil, nil)
	if missing(err) {
		return nil
	}
	return err
}

func (d *docker) rename(ctx context.Context, name, to string) error {
	return d.call(ctx, http.MethodPost, "/containers/"+url.PathEscape(name)+"/rename?name="+url.QueryEscape(to), nil, nil)
}

// settle waits until the new container is known to work: healthy when it
// has a health check, otherwise up for a while without restarting.
func (d *docker) settle(ctx context.Context, name string, patience, steady time.Duration) error {
	began := time.Now()
	for {
		c, err := d.inspect(ctx, name)
		if err != nil {
			return err
		}
		switch {
		case c.RestartCount > 0 || c.State.Restarting:
			return errors.New("the new version does not start")
		case c.State.Health != nil && c.State.Health.Status == "healthy":
			return nil
		case c.State.Health != nil && c.State.Health.Status == "unhealthy":
			return errors.New("the new version started but does not work")
		case c.State.Health == nil && !c.State.Running:
			return errors.New("the new version does not start")
		case c.State.Health == nil && time.Since(began) >= steady:
			return nil
		case time.Since(began) > patience:
			return errors.New("the new version did not come up in time")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
