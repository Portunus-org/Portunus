package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters follow the OWASP minimum (m=19 MiB, t=2, p=1): strong
// enough with the login rate limiter in front, and cheap enough that a burst
// of logins cannot exhaust a 1 GB VPS.
const (
	argonMemoryKiB = 19 * 1024
	argonTime      = 2
	argonThreads   = 1
	argonKeyLen    = 32
	argonSaltLen   = 16
)

// hashSem bounds concurrent argon2 computations, so a burst of logins from
// many addresses costs at most GOMAXPROCS × argonMemoryKiB of RAM.
var hashSem = make(chan struct{}, max(1, runtime.GOMAXPROCS(0)))

// argonKey runs argon2id under hashSem. Each call allocates a large
// short-lived block; with an otherwise tiny heap the GC would not run for
// minutes, so memory is handed back to the OS shortly after the last hash.
func argonKey(password, salt []byte, time, mem uint32, threads uint8, keyLen uint32) []byte {
	hashSem <- struct{}{}
	defer func() {
		<-hashSem
		scheduleRelease()
	}()
	return argon2.IDKey(password, salt, time, mem, threads, keyLen)
}

var (
	releaseMu    sync.Mutex
	releaseTimer *time.Timer
)

func scheduleRelease() {
	releaseMu.Lock()
	defer releaseMu.Unlock()
	if releaseTimer == nil {
		releaseTimer = time.AfterFunc(2*time.Second, debug.FreeOSMemory)
		return
	}
	releaseTimer.Reset(2 * time.Second)
}

const (
	MinPasswordLength = 12
	MaxPasswordLength = 256
)

var ErrWeakPassword = fmt.Errorf("password must be %d–%d characters", MinPasswordLength, MaxPasswordLength)

// HashPassword returns a PHC-formatted argon2id hash.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argonKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword checks password against a hash produced by HashPassword.
// Parameters are read from the hash, so old hashes keep working if the
// defaults above change.
func VerifyPassword(hash, password string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("unsupported password hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("unsupported argon2 version")
	}
	var (
		mem     uint32
		time    uint32
		threads uint8
	)
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &time, &threads); err != nil {
		return false, fmt.Errorf("parse argon2 params: %w", err)
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got := argonKey([]byte(password), salt, time, mem, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func ValidatePassword(password string) error {
	if n := len([]rune(password)); n < MinPasswordLength || n > MaxPasswordLength {
		return ErrWeakPassword
	}
	return nil
}

// GeneratePassword returns a random password from an unambiguous alphabet,
// used for the bootstrap admin and password resets.
func GeneratePassword() string {
	const alphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789"
	return randomString(alphabet, 20)
}

// randomString draws n characters uniformly from alphabet, rejecting bytes
// that would introduce modulo bias.
func randomString(alphabet string, n int) string {
	limit := 256 - 256%len(alphabet)
	out := make([]byte, 0, n)
	var buf [32]byte
	for len(out) < n {
		rand.Read(buf[:]) // never fails on supported platforms
		for _, c := range buf {
			if int(c) < limit && len(out) < n {
				out = append(out, alphabet[int(c)%len(alphabet)])
			}
		}
	}
	return string(out)
}
