// tools/png2sprite/main.go — PNG to terminal-sprite converter.
//
// Converts a pixel-art PNG to a rune grid + palette suitable for use with
// RenderSprite or RenderBrailleSprite in internal/client/art.go.
//
// Usage:
//
//	go run ./tools/png2sprite -in <file.png> [-width 32] [-colors 12] \
//	    [-bg-tolerance 30] [-out text|go] [-name hotDog] [-style halfblock|braille]
package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/png"
	"math"
	"os"
	"sort"
	"strings"
)

// ─── Flags ────────────────────────────────────────────────────────────────────

var (
	flagIn     = flag.String("in", "", "input PNG file (required)")
	flagWidth  = flag.Int("width", 32, "target sprite width in pixels (for braille: pixel width; each cell=2px wide)")
	flagColors = flag.Int("colors", 12, "max palette size (transparent not counted)")
	flagBGTol  = flag.Int("bg-tolerance", 30, "background removal tolerance 0-255")
	flagOut    = flag.String("out", "text", "output format: text or go")
	flagName   = flag.String("name", "hotDog", "Go variable name prefix (used with -out go)")
	flagStyle  = flag.String("style", "halfblock", "rendering style: halfblock or braille")
)

// ─── Color helpers ────────────────────────────────────────────────────────────

type rgb struct{ r, g, b uint8 }

func colorDist(a, b rgb) float64 {
	dr := float64(a.r) - float64(b.r)
	dg := float64(a.g) - float64(b.g)
	db := float64(a.b) - float64(b.b)
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

// sampleCornerBG averages the four image corners to derive the background color.
func sampleCornerBG(img image.Image) rgb {
	b := img.Bounds()
	corners := [][2]int{
		{b.Min.X, b.Min.Y},
		{b.Max.X - 1, b.Min.Y},
		{b.Min.X, b.Max.Y - 1},
		{b.Max.X - 1, b.Max.Y - 1},
	}
	var sumR, sumG, sumB int
	for _, c := range corners {
		r, g, bl, _ := img.At(c[0], c[1]).RGBA()
		sumR += int(r >> 8)
		sumG += int(g >> 8)
		sumB += int(bl >> 8)
	}
	return rgb{uint8(sumR / 4), uint8(sumG / 4), uint8(sumB / 4)}
}

// isBG returns true if c is within tolerance of the background color.
// Also treats light-gray pixels (low saturation, high brightness) as background
// when they are close to typical canvas grays.
func isBG(c rgb, bg rgb, tol int) bool {
	if colorDist(c, bg) <= float64(tol) {
		return true
	}
	// Also treat low-saturation gray-ish pixels near the expected canvas range
	// as background (handles slight gradients in canvas fills).
	maxC := max3(int(c.r), int(c.g), int(c.b))
	minC := min3(int(c.r), int(c.g), int(c.b))
	saturation := maxC - minC
	if saturation <= 10 && maxC >= 150 {
		// It's a near-gray bright pixel; also check it's close to the bg brightness
		bgBrightness := (int(bg.r) + int(bg.g) + int(bg.b)) / 3
		cBrightness := (int(c.r) + int(c.g) + int(c.b)) / 3
		if abs(cBrightness-bgBrightness) <= tol+60 {
			return true
		}
	}
	return false
}

// ─── Background removal & trim ────────────────────────────────────────────────

// pixelGrid is a W×H grid of rgb values; transparent pixels have alpha==transparent.
type pixelGrid struct {
	w, h int
	data []rgb // row-major; transparent indicated by sentinel
	alpha []bool // true = opaque
}

func loadAndRemoveBG(img image.Image, bg rgb, tol int) pixelGrid {
	b := img.Bounds()
	w, h := b.Max.X-b.Min.X, b.Max.Y-b.Min.Y
	data := make([]rgb, w*h)
	alpha := make([]bool, w*h)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			c := rgb{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8)}
			idx := y*w + x
			if a>>8 < 128 || isBG(c, bg, tol) {
				data[idx] = rgb{}
				alpha[idx] = false
			} else {
				data[idx] = c
				alpha[idx] = true
			}
		}
	}
	return pixelGrid{w: w, h: h, data: data, alpha: alpha}
}

func (g *pixelGrid) at(x, y int) (rgb, bool) {
	idx := y*g.w + x
	return g.data[idx], g.alpha[idx]
}

// trim removes fully-transparent margins.
func (g *pixelGrid) trim() pixelGrid {
	minX, minY := g.w, g.h
	maxX, maxY := 0, 0
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			if _, ok := g.at(x, y); ok {
				if x < minX { minX = x }
				if x > maxX { maxX = x }
				if y < minY { minY = y }
				if y > maxY { maxY = y }
			}
		}
	}
	if minX > maxX {
		return pixelGrid{w: 0, h: 0}
	}
	nw := maxX - minX + 1
	nh := maxY - minY + 1
	data := make([]rgb, nw*nh)
	alpha := make([]bool, nw*nh)
	for y := 0; y < nh; y++ {
		for x := 0; x < nw; x++ {
			c, ok := g.at(minX+x, minY+y)
			idx := y*nw + x
			data[idx] = c
			alpha[idx] = ok
		}
	}
	return pixelGrid{w: nw, h: nh, data: data, alpha: alpha}
}

// ─── Downscale ────────────────────────────────────────────────────────────────

// downscale box-averages the pixel grid to targetW columns.
// Height is derived to preserve aspect ratio and is padded to even.
func downscale(g pixelGrid, targetW int) pixelGrid {
	if g.w == 0 || g.h == 0 {
		return g
	}
	targetH := int(math.Round(float64(g.h) * float64(targetW) / float64(g.w)))
	if targetH < 1 {
		targetH = 1
	}
	if targetH%2 != 0 {
		targetH++ // pad to even for half-block rendering
	}

	data := make([]rgb, targetW*targetH)
	alpha := make([]bool, targetW*targetH)

	for ty := 0; ty < targetH; ty++ {
		for tx := 0; tx < targetW; tx++ {
			// Source rectangle (sub-pixel accuracy)
			x0 := float64(tx) * float64(g.w) / float64(targetW)
			x1 := float64(tx+1) * float64(g.w) / float64(targetW)
			y0 := float64(ty) * float64(g.h) / float64(targetH)
			y1 := float64(ty+1) * float64(g.h) / float64(targetH)

			var sumR, sumG, sumB, sumW float64
			var opaqueW float64

			for sy := int(y0); sy <= int(y1) && sy < g.h; sy++ {
				yWeight := 1.0
				if float64(sy) < y0 {
					yWeight = float64(sy+1) - y0
				} else if float64(sy+1) > y1 {
					yWeight = y1 - float64(sy)
				}
				for sx := int(x0); sx <= int(x1) && sx < g.w; sx++ {
					xWeight := 1.0
					if float64(sx) < x0 {
						xWeight = float64(sx+1) - x0
					} else if float64(sx+1) > x1 {
						xWeight = x1 - float64(sx)
					}
					w := xWeight * yWeight
					c, ok := g.at(sx, sy)
					if ok {
						sumR += float64(c.r) * w
						sumG += float64(c.g) * w
						sumB += float64(c.b) * w
						opaqueW += w
					}
					sumW += w
				}
			}

			idx := ty*targetW + tx
			// Consider this pixel opaque if more than 40% of its source area was opaque.
			if sumW > 0 && opaqueW/sumW > 0.40 {
				data[idx] = rgb{
					uint8(sumR / opaqueW),
					uint8(sumG / opaqueW),
					uint8(sumB / opaqueW),
				}
				alpha[idx] = true
			}
		}
	}
	return pixelGrid{w: targetW, h: targetH, data: data, alpha: alpha}
}

// ─── Braille downscale ────────────────────────────────────────────────────────

// downscaleBraille box-averages the pixel grid to targetPixelW columns.
// For braille rendering each terminal cell is 2 px wide × 4 px tall,
// so the pixel height is padded to a multiple of 4 (and aspect-ratio derived).
func downscaleBraille(g pixelGrid, targetPixelW int) pixelGrid {
	if g.w == 0 || g.h == 0 {
		return g
	}
	// Derive height preserving aspect ratio.
	targetH := int(math.Round(float64(g.h) * float64(targetPixelW) / float64(g.w)))
	if targetH < 1 {
		targetH = 1
	}
	// Pad to multiple of 4 (braille cell height = 4 px).
	if targetH%4 != 0 {
		targetH += 4 - (targetH % 4)
	}

	data := make([]rgb, targetPixelW*targetH)
	alpha := make([]bool, targetPixelW*targetH)

	for ty := 0; ty < targetH; ty++ {
		for tx := 0; tx < targetPixelW; tx++ {
			x0 := float64(tx) * float64(g.w) / float64(targetPixelW)
			x1 := float64(tx+1) * float64(g.w) / float64(targetPixelW)
			y0 := float64(ty) * float64(g.h) / float64(targetH)
			y1 := float64(ty+1) * float64(g.h) / float64(targetH)

			var sumR, sumG, sumB, sumW float64
			var opaqueW float64

			for sy := int(y0); sy <= int(y1) && sy < g.h; sy++ {
				yWeight := 1.0
				if float64(sy) < y0 {
					yWeight = float64(sy+1) - y0
				} else if float64(sy+1) > y1 {
					yWeight = y1 - float64(sy)
				}
				for sx := int(x0); sx <= int(x1) && sx < g.w; sx++ {
					xWeight := 1.0
					if float64(sx) < x0 {
						xWeight = float64(sx+1) - x0
					} else if float64(sx+1) > x1 {
						xWeight = x1 - float64(sx)
					}
					w := xWeight * yWeight
					c, ok := g.at(sx, sy)
					if ok {
						sumR += float64(c.r) * w
						sumG += float64(c.g) * w
						sumB += float64(c.b) * w
						opaqueW += w
					}
					sumW += w
				}
			}

			idx := ty*targetPixelW + tx
			if sumW > 0 && opaqueW/sumW > 0.35 {
				data[idx] = rgb{
					uint8(sumR / opaqueW),
					uint8(sumG / opaqueW),
					uint8(sumB / opaqueW),
				}
				alpha[idx] = true
			}
		}
	}
	return pixelGrid{w: targetPixelW, h: targetH, data: data, alpha: alpha}
}

// brailleGrids converts a pixel grid (width=W, height=H, both multiples of 2
// and 4 respectively) into:
//   - dotRows: H rows of W '#'/'.' characters (opaque=# transparent=.)
//   - cellColorRows: (H/4) rows of (W/2) palette runes — the dominant
//     xterm color of each 2×4 cell's opaque pixels; '.' = fully transparent cell
//   - palette: map[rune]string of xterm256 codes
//
// The pixel grid must already be downscaled to the target braille dimensions.
func brailleGrids(g *pixelGrid, centers []rgb, pe []paletteEntry) (dotRows []string, cellColorRows []string, cellPal map[rune]string) {
	cellW := g.w / 2 // number of braille cells per row
	cellH := g.h / 4 // number of braille cell rows

	// Build dot mask rows (one per pixel row).
	dotRows = make([]string, g.h)
	for py := 0; py < g.h; py++ {
		var sb strings.Builder
		for px := 0; px < g.w; px++ {
			_, ok := g.at(px, py)
			if ok {
				sb.WriteByte('#')
			} else {
				sb.WriteByte('.')
			}
		}
		dotRows[py] = sb.String()
	}

	// Build per-cell dominant color.
	// For each 2×4 cell, collect opaque pixels, find the most common cluster.
	cellColorRows = make([]string, cellH)
	cellPal = make(map[rune]string)

	// Use a separate rune alphabet for cell colors to avoid collision with
	// the dot mask '.' and '#' characters.
	const cellAlpha = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	// Map xterm code → rune assignment (same strategy as buildPalette).
	codeToRune := make(map[int]rune)
	runeIdx := 0

	for cy := 0; cy < cellH; cy++ {
		var sb strings.Builder
		for cx := 0; cx < cellW; cx++ {
			// Collect opaque pixels in this 2×4 cell.
			clusterCount := make(map[int]int) // cluster index → count
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					px := cx*2 + dx
					py := cy*4 + dy
					c, ok := g.at(px, py)
					if ok {
						ci := nearestCenter(c, centers)
						clusterCount[ci]++
					}
				}
			}
			if len(clusterCount) == 0 {
				// Fully transparent cell.
				sb.WriteByte('.')
				continue
			}
			// Find dominant cluster.
			bestCI, bestCount := 0, -1
			for ci, cnt := range clusterCount {
				if cnt > bestCount {
					bestCount = cnt
					bestCI = ci
				}
			}
			code := pe[bestCI].code

			// Assign a rune for this xterm code.
			r, exists := codeToRune[code]
			if !exists {
				r = rune(cellAlpha[runeIdx%len(cellAlpha)])
				// Ensure uniqueness: bump runeIdx until we find an unused rune.
				for runeIdx < len(cellAlpha) {
					candidate := rune(cellAlpha[runeIdx%len(cellAlpha)])
					alreadyUsed := false
					for _, used := range codeToRune {
						if used == candidate {
							alreadyUsed = true
							break
						}
					}
					if !alreadyUsed {
						r = candidate
						break
					}
					runeIdx++
				}
				codeToRune[code] = r
				cellPal[r] = fmt.Sprintf("%d", code)
				runeIdx++
			}
			sb.WriteRune(r)
		}
		cellColorRows[cy] = sb.String()
	}

	return dotRows, cellColorRows, cellPal
}

// emitBrailleText prints the dot rows, cell color rows, and palette in text form.
func emitBrailleText(dotRows, cellColorRows []string, cellPal map[rune]string) {
	fmt.Printf("=== Braille dot mask (%d px rows × %d px cols) ===\n", len(dotRows), func() int {
		if len(dotRows) > 0 {
			return len(dotRows[0])
		}
		return 0
	}())
	for _, r := range dotRows {
		fmt.Println(r)
	}
	fmt.Printf("\n=== Braille cell colors (%d cell rows × %d cell cols) ===\n",
		len(cellColorRows), func() int {
			if len(cellColorRows) > 0 {
				return len([]rune(cellColorRows[0]))
			}
			return 0
		}())
	for _, r := range cellColorRows {
		fmt.Println(r)
	}
	fmt.Println("\n=== Cell palette (rune → xterm256 code) ===")
	// Print sorted for determinism.
	type kv struct {
		r    rune
		code string
	}
	var pairs []kv
	for r, code := range cellPal {
		pairs = append(pairs, kv{r, code})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].r < pairs[j].r })
	for _, p := range pairs {
		fmt.Printf("  '%c' → %s\n", p.r, p.code)
	}
}

// emitBrailleGo prints Go variable declarations for the braille sprite.
func emitBrailleGo(dotRows, cellColorRows []string, cellPal map[rune]string, name, cmd string) {
	fmt.Printf("// Generated by tools/png2sprite — braille style\n")
	fmt.Printf("// Command: %s\n", cmd)
	fmt.Printf("var %sBrailleDots = []string{\n", name)
	for _, r := range dotRows {
		fmt.Printf("\t%q,\n", r)
	}
	fmt.Println("}")
	fmt.Println()
	fmt.Printf("var %sBrailleCells = []string{\n", name)
	for _, r := range cellColorRows {
		fmt.Printf("\t%q,\n", r)
	}
	fmt.Println("}")
	fmt.Println()
	fmt.Printf("var %sBraillePalette = map[rune]string{\n", name)
	// Sort for determinism.
	type kv struct {
		r    rune
		code string
	}
	var pairs []kv
	for r, code := range cellPal {
		pairs = append(pairs, kv{r, code})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].r < pairs[j].r })
	for _, p := range pairs {
		fmt.Printf("\t'%c': %q,\n", p.r, p.code)
	}
	fmt.Println("}")
}

// ─── Color quantization ───────────────────────────────────────────────────────

// kmeansQuantize runs k-means over opaque pixels and returns cluster centers.
func kmeansQuantize(g *pixelGrid, k int) []rgb {
	// Collect opaque pixels.
	var pixels []rgb
	for i, ok := range g.alpha {
		if ok {
			pixels = append(pixels, g.data[i])
		}
	}
	if len(pixels) == 0 {
		return nil
	}
	if len(pixels) <= k {
		return pixels
	}

	// Seed centers by picking evenly spaced pixels (fast deterministic init).
	centers := make([]rgb, k)
	step := len(pixels) / k
	for i := 0; i < k; i++ {
		centers[i] = pixels[i*step]
	}

	labels := make([]int, len(pixels))
	for iter := 0; iter < 20; iter++ {
		changed := false
		// Assign.
		for i, p := range pixels {
			best, bestDist := 0, math.MaxFloat64
			for ci, c := range centers {
				if d := colorDist(p, c); d < bestDist {
					bestDist = d
					best = ci
				}
			}
			if labels[i] != best {
				labels[i] = best
				changed = true
			}
		}
		if !changed {
			break
		}
		// Update centers.
		sums := make([][3]float64, k)
		counts := make([]int, k)
		for i, p := range pixels {
			l := labels[i]
			sums[l][0] += float64(p.r)
			sums[l][1] += float64(p.g)
			sums[l][2] += float64(p.b)
			counts[l]++
		}
		for ci := range centers {
			if counts[ci] > 0 {
				centers[ci] = rgb{
					uint8(sums[ci][0] / float64(counts[ci])),
					uint8(sums[ci][1] / float64(counts[ci])),
					uint8(sums[ci][2] / float64(counts[ci])),
				}
			}
		}
	}
	return centers
}

// nearestCenter returns the index of the closest center to p.
func nearestCenter(p rgb, centers []rgb) int {
	best, bestDist := 0, math.MaxFloat64
	for i, c := range centers {
		if d := colorDist(p, c); d < bestDist {
			bestDist = d
			best = i
		}
	}
	return best
}

// ─── xterm-256 color mapping ──────────────────────────────────────────────────

// xterm256 returns the xterm-256 color index closest to the given RGB.
// It checks the 216-color cube (indices 16-231) and the 24 grayscale ramp (232-255).
func xterm256(c rgb) (int, rgb) {
	best, bestDist := 0, math.MaxFloat64

	// Helper: evaluate a candidate.
	check := func(idx int, cr, cg, cb uint8) {
		candidate := rgb{cr, cg, cb}
		d := colorDist(c, candidate)
		if d < bestDist {
			bestDist = d
			best = idx
		}
	}

	// 216-color cube: index = 16 + 36*r + 6*g + b  (each component 0-5 → 0,95,135,175,215,255)
	cubeVals := [6]uint8{0, 95, 135, 175, 215, 255}
	for ri := 0; ri < 6; ri++ {
		for gi := 0; gi < 6; gi++ {
			for bi := 0; bi < 6; bi++ {
				check(16+36*ri+6*gi+bi, cubeVals[ri], cubeVals[gi], cubeVals[bi])
			}
		}
	}

	// 24-step grayscale ramp: indices 232-255 → 8,18,28,...,238
	for i := 0; i < 24; i++ {
		v := uint8(8 + 10*i)
		check(232+i, v, v, v)
	}

	// Also check standard 16 colors (rough approximations).
	std16 := []rgb{
		{0, 0, 0}, {128, 0, 0}, {0, 128, 0}, {128, 128, 0},
		{0, 0, 128}, {128, 0, 128}, {0, 128, 128}, {192, 192, 192},
		{128, 128, 128}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
		{0, 0, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
	}
	for i, sc := range std16 {
		check(i, sc.r, sc.g, sc.b)
	}

	// Return the best index and its approximate RGB.
	return best, xterm256ToRGB(best)
}

// xterm256ToRGB converts an xterm-256 index back to approximate RGB.
func xterm256ToRGB(idx int) rgb {
	if idx < 16 {
		std16 := []rgb{
			{0, 0, 0}, {128, 0, 0}, {0, 128, 0}, {128, 128, 0},
			{0, 0, 128}, {128, 0, 128}, {0, 128, 128}, {192, 192, 192},
			{128, 128, 128}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
			{0, 0, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
		}
		return std16[idx]
	}
	if idx >= 232 {
		v := uint8(8 + 10*(idx-232))
		return rgb{v, v, v}
	}
	i := idx - 16
	cubeVals := [6]uint8{0, 95, 135, 175, 215, 255}
	return rgb{cubeVals[i/36], cubeVals[(i/6)%6], cubeVals[i%6]}
}

// ─── Rune assignment ──────────────────────────────────────────────────────────

// rune alphabet for palette entries (avoiding '.' and ' ' which mean transparent).
const runeAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// buildPalette maps cluster index → {rune, xterm256 code, approx RGB}.
// Clusters that quantize to the same xterm code are merged: all such cluster
// indices share the same rune so the resulting palette has no duplicate codes.
type paletteEntry struct {
	r      rune
	code   int
	approx rgb
}

func buildPalette(centers []rgb) []paletteEntry {
	entries := make([]paletteEntry, len(centers))

	// Map each center to its xterm code.
	type pair struct {
		ci     int
		code   int
		approx rgb
	}
	pairs := make([]pair, len(centers))
	for i, c := range centers {
		code, approx := xterm256(c)
		pairs[i] = pair{i, code, approx}
	}

	// Sort by xterm code for deterministic output; stable-sort preserves original
	// cluster order among ties so rune assignment is reproducible.
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].code < pairs[j].code })

	// Assign runes, merging clusters with duplicate codes.
	runeIdx := 0
	for i, p := range pairs {
		if i > 0 && pairs[i].code == pairs[i-1].code {
			// Same xterm code as previous cluster: reuse the same rune.
			entries[p.ci] = entries[pairs[i-1].ci]
		} else {
			r := rune(runeAlphabet[runeIdx%len(runeAlphabet)])
			entries[p.ci] = paletteEntry{r: r, code: p.code, approx: p.approx}
			runeIdx++
		}
	}
	return entries
}

// ─── Grid → rune grid ─────────────────────────────────────────────────────────

func buildRuneGrid(g *pixelGrid, centers []rgb, palette []paletteEntry) []string {
	rows := make([]string, g.h)
	for y := 0; y < g.h; y++ {
		var sb strings.Builder
		for x := 0; x < g.w; x++ {
			c, ok := g.at(x, y)
			if !ok {
				sb.WriteRune('.')
			} else {
				ci := nearestCenter(c, centers)
				sb.WriteRune(palette[ci].r)
			}
		}
		rows[y] = sb.String()
	}
	return rows
}

// ─── Output formatters ────────────────────────────────────────────────────────

// uniquePalette returns only the distinct (rune, code) pairs from the palette,
// deduplicating entries that share the same rune (merged clusters).
func uniquePalette(palette []paletteEntry) []paletteEntry {
	seen := map[rune]bool{}
	var out []paletteEntry
	for _, e := range palette {
		if !seen[e.r] {
			seen[e.r] = true
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].code < out[j].code })
	return out
}

func emitText(rows []string, palette []paletteEntry, centers []rgb) {
	fmt.Println("=== Letter mask ===")
	for _, r := range rows {
		fmt.Println(r)
	}
	fmt.Println()
	fmt.Println("=== Palette (rune → xterm256 code, approx RGB) ===")
	for _, e := range uniquePalette(palette) {
		fmt.Printf("  '%c' → %d  approx RGB(%d,%d,%d)\n",
			e.r, e.code, e.approx.r, e.approx.g, e.approx.b)
	}
}

func emitGo(rows []string, palette []paletteEntry, centers []rgb, name string, cmd string) {
	fmt.Printf("// Generated by tools/png2sprite\n")
	fmt.Printf("// Command: %s\n", cmd)
	fmt.Printf("var %sSprite = []string{\n", name)
	for _, r := range rows {
		fmt.Printf("\t%q,\n", r)
	}
	fmt.Println("}")
	fmt.Println()
	fmt.Printf("var %sPalette = map[rune]string{\n", name)
	for _, e := range uniquePalette(palette) {
		fmt.Printf("\t'%c': %q, // approx RGB(%d,%d,%d)\n",
			e.r, fmt.Sprintf("%d", e.code),
			e.approx.r, e.approx.g, e.approx.b)
	}
	fmt.Println("}")
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func max3(a, b, c int) int {
	if a > b {
		if a > c {
			return a
		}
		return c
	}
	if b > c {
		return b
	}
	return c
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	flag.Parse()

	if *flagIn == "" {
		fmt.Fprintln(os.Stderr, "png2sprite: -in is required")
		flag.Usage()
		os.Exit(1)
	}
	if *flagOut != "text" && *flagOut != "go" {
		fmt.Fprintln(os.Stderr, "png2sprite: -out must be 'text' or 'go'")
		os.Exit(1)
	}
	if *flagStyle != "halfblock" && *flagStyle != "braille" {
		fmt.Fprintln(os.Stderr, "png2sprite: -style must be 'halfblock' or 'braille'")
		os.Exit(1)
	}

	// Load PNG.
	f, err := os.Open(*flagIn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "png2sprite: open %s: %v\n", *flagIn, err)
		os.Exit(1)
	}
	img, _, err := image.Decode(f)
	_ = f.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "png2sprite: decode %s: %v\n", *flagIn, err)
		os.Exit(1)
	}

	// Sample background from corners.
	bg := sampleCornerBG(img)

	// Remove background and trim.
	grid := loadAndRemoveBG(img, bg, *flagBGTol)
	grid = grid.trim()

	if grid.w == 0 {
		fmt.Fprintln(os.Stderr, "png2sprite: after bg removal, no opaque pixels remain — try raising -bg-tolerance")
		os.Exit(1)
	}

	// Reconstruct the original command for reference.
	cmd := fmt.Sprintf("go run ./tools/png2sprite -in %s -width %d -colors %d -bg-tolerance %d -out %s -name %s -style %s",
		*flagIn, *flagWidth, *flagColors, *flagBGTol, *flagOut, *flagName, *flagStyle)

	if *flagStyle == "braille" {
		// Braille pipeline: pixel width = flagWidth; each cell = 2px wide × 4px tall.
		scaled := downscaleBraille(grid, *flagWidth)

		centers := kmeansQuantize(&scaled, *flagColors)
		if len(centers) == 0 {
			fmt.Fprintln(os.Stderr, "png2sprite: no opaque pixels after downscale")
			os.Exit(1)
		}

		palette := buildPalette(centers)
		dotRows, cellColorRows, cellPal := brailleGrids(&scaled, centers, palette)

		switch *flagOut {
		case "go":
			emitBrailleGo(dotRows, cellColorRows, cellPal, *flagName, cmd)
		default:
			emitBrailleText(dotRows, cellColorRows, cellPal)
		}
		return
	}

	// Halfblock pipeline (original).
	scaled := downscale(grid, *flagWidth)

	centers := kmeansQuantize(&scaled, *flagColors)
	if len(centers) == 0 {
		fmt.Fprintln(os.Stderr, "png2sprite: no opaque pixels after downscale")
		os.Exit(1)
	}

	palette := buildPalette(centers)
	rows := buildRuneGrid(&scaled, centers, palette)

	switch *flagOut {
	case "go":
		emitGo(rows, palette, centers, *flagName, cmd)
	default:
		emitText(rows, palette, centers)
	}
}
