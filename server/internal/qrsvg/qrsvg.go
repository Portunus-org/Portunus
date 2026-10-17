// Package qrsvg renders QR codes as compact SVG. Modules are drawn with
// currentColor so the UI can theme them.
package qrsvg

import (
	"fmt"
	"strings"

	"rsc.io/qr"
)

const quiet = 4 // quiet-zone width in modules, per the QR spec

// Encode returns an SVG QR code for text (error correction level M).
func Encode(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	n := code.Size + 2*quiet
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`, n, n)
	b.WriteString(`<path fill="currentColor" d="`)
	for y := 0; y < code.Size; y++ {
		// Merge horizontal runs of dark modules into single rectangles.
		for x := 0; x < code.Size; {
			if !code.Black(x, y) {
				x++
				continue
			}
			start := x
			for x < code.Size && code.Black(x, y) {
				x++
			}
			fmt.Fprintf(&b, "M%d %dh%dv1h-%dz", start+quiet, y+quiet, x-start, x-start)
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String(), nil
}
