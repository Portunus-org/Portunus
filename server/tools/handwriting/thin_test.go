package main

import "testing"

func TestThinBarKeepsCentreLine(t *testing.T) {
	g := &grid{w: 60, h: 21, v: make([]bool, 60*21)}
	for y := 5; y <= 15; y++ { // 11 px thick, centre row 10
		for x := 5; x < 55; x++ {
			g.set(x, y, true)
		}
	}
	s := thin(g)
	rows := map[int]int{}
	for y := 0; y < s.h; y++ {
		for x := 0; x < s.w; x++ {
			if s.at(x, y) {
				rows[y]++
			}
		}
	}
	t.Logf("skeleton pixels per row: %v", rows)
	if rows[10] < 30 {
		t.Fatalf("skeleton is not on the centre row: %v", rows)
	}
}

func TestThinDiskShrinksToCentre(t *testing.T) {
	g := &grid{w: 61, h: 61, v: make([]bool, 61*61)}
	for y := 0; y < 61; y++ {
		for x := 0; x < 61; x++ {
			if (x-30)*(x-30)+(y-30)*(y-30) <= 20*20 {
				g.set(x, y, true)
			}
		}
	}
	s := thin(g)
	sx, sy, n := 0, 0, 0
	for y := 0; y < s.h; y++ {
		for x := 0; x < s.w; x++ {
			if s.at(x, y) {
				sx, sy, n = sx+x, sy+y, n+1
			}
		}
	}
	t.Logf("skeleton: %d px, centroid (%d, %d)", n, sx/max(n, 1), sy/max(n, 1))
	if n == 0 || abs(sx/n-30) > 3 || abs(sy/n-30) > 3 {
		t.Fatalf("disk skeleton not near the centre")
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
