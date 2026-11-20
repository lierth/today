// Command gen draws the Today logo and renders every icon the app ships.
//
// Run it from the repository root:
//
//	go run ./resources/gen
//
// One silhouette, one source of truth. The snail is a list of Bézier segments
// in a 44×44 grid (the grid ~/.claude/skills/tools-ui-design/SKILL.md §8 asks
// for). The same description writes the SVG files and rasterises every PNG, so
// the vector art and the bitmaps can never drift apart. Nothing here needs a
// network, a browser, or a dependency outside the standard library.
//
// Outputs:
//
//	resources/logo.svg              full detail, 32px and up
//	resources/logo-small.svg        fewer nodes, 14–24px
//	resources/icon.svg              1024 squircle app icon
//	resources/tray.svg              18×18 menu-bar template
//	resources/favicon.svg           light + dark in one file
//	resources/icon.png              1024, what mygo.json packages
//	resources/tray.png / tray@2x.png
//	resources/favicon-light.png / favicon-dark.png
//	resources/apple-touch-icon.png
//	firmware/src/logo_inc.h         boot-screen bitmap
//	/tmp/todayui/hw13/logo-*.png    previews for review
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type pt struct{ x, y float64 }

func (a pt) sub(b pt) pt        { return pt{a.x - b.x, a.y - b.y} }
func (a pt) add(b pt) pt        { return pt{a.x + b.x, a.y + b.y} }
func (a pt) scale(k float64) pt { return pt{a.x * k, a.y * k} }
func (a pt) len() float64       { return math.Hypot(a.x, a.y) }
func (a pt) rot90() pt          { return pt{-a.y, a.x} }
func dist(a, b pt) float64      { return a.sub(b).len() }
func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}

// segDist is the distance from p to the segment ab.
func segDist(p, a, b pt) float64 {
	ab := b.sub(a)
	den := ab.x*ab.x + ab.y*ab.y
	if den == 0 {
		return dist(p, a)
	}
	ap := p.sub(a)
	t := clamp((ap.x*ab.x+ap.y*ab.y)/den, 0, 1)
	return dist(p, pt{a.x + ab.x*t, a.y + ab.y*t})
}

// ---------------------------------------------------------------- paths

// A path is a closed outline of line and cubic segments. It is both written to
// SVG and flattened for rasterising.
type path struct {
	start pt
	segs  []seg
}

type seg struct {
	c1, c2, to pt // c1==c2==zero for a line
	line       bool
}

func (p *path) line(to pt) { p.segs = append(p.segs, seg{to: to, line: true}) }

func (p *path) curve(c1, c2, to pt) { p.segs = append(p.segs, seg{c1: c1, c2: c2, to: to}) }

func (p path) flatten(perSeg int) []pt {
	out := []pt{p.start}
	cur := p.start
	for _, s := range p.segs {
		if s.line {
			out = append(out, s.to)
			cur = s.to
			continue
		}
		for i := 1; i <= perSeg; i++ {
			t := float64(i) / float64(perSeg)
			u := 1 - t
			x := u*u*u*cur.x + 3*u*u*t*s.c1.x + 3*u*t*t*s.c2.x + t*t*t*s.to.x
			y := u*u*u*cur.y + 3*u*u*t*s.c1.y + 3*u*t*t*s.c2.y + t*t*t*s.to.y
			out = append(out, pt{x, y})
		}
		cur = s.to
	}
	return out
}

func (p path) svg(digits int) string {
	f := func(v float64) string {
		s := strconv.FormatFloat(v, 'f', digits, 64)
		s = strings.TrimRight(s, "0")
		return strings.TrimSuffix(s, ".")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "M%s %s", f(p.start.x), f(p.start.y))
	for _, s := range p.segs {
		if s.line {
			fmt.Fprintf(&b, "L%s %s", f(s.to.x), f(s.to.y))
			continue
		}
		fmt.Fprintf(&b, "C%s %s %s %s %s %s",
			f(s.c1.x), f(s.c1.y), f(s.c2.x), f(s.c2.y), f(s.to.x), f(s.to.y))
	}
	b.WriteString("Z")
	return b.String()
}

func reversed(p []pt) []pt {
	out := make([]pt, len(p))
	for i := range p {
		out[len(p)-1-i] = p[i]
	}
	return out
}

func signedArea(p []pt) float64 {
	var a float64
	for i := range p {
		j := (i + 1) % len(p)
		a += p[i].x*p[j].y - p[j].x*p[i].y
	}
	return a / 2
}

// compound emits several subpaths as one path with nonzero fill: solids wound
// one way, holes the other. That keeps the logo a single compound path whose
// interior detail is carved out by winding, not painted over in white.
func compound(solid, holes [][]pt, digits int) string {
	var b strings.Builder
	f := func(v float64) string {
		s := strconv.FormatFloat(v, 'f', digits, 64)
		s = strings.TrimRight(s, "0")
		return strings.TrimSuffix(s, ".")
	}
	emit := func(p []pt, positive bool) {
		if (signedArea(p) < 0) == positive {
			p = reversed(p)
		}
		fmt.Fprintf(&b, "M%s %s", f(p[0].x), f(p[0].y))
		for _, q := range p[1:] {
			fmt.Fprintf(&b, "L%s %s", f(q.x), f(q.y))
		}
		b.WriteString("Z")
	}
	for _, p := range solid {
		emit(simplify(p, 0.05), true)
	}
	for _, p := range holes {
		emit(simplify(p, 0.04), false)
	}
	return b.String()
}

// simplify drops points a straight line already passes through. The rasteriser
// works on the dense polygons, but the SVG only needs enough points to stay
// inside `tol` of them; without this the files run to tens of kilobytes.
func simplify(p []pt, tol float64) []pt {
	if len(p) < 4 {
		return p
	}
	keep := make([]bool, len(p))
	keep[0], keep[len(p)-1] = true, true
	var walk func(lo, hi int)
	walk = func(lo, hi int) {
		if hi <= lo+1 {
			return
		}
		worst, at := 0.0, -1
		for i := lo + 1; i < hi; i++ {
			if d := segDist(p[i], p[lo], p[hi]); d > worst {
				worst, at = d, i
			}
		}
		if worst > tol && at > 0 {
			keep[at] = true
			walk(lo, at)
			walk(at, hi)
		}
	}
	// Split the ring at its extremes so the two halves simplify independently.
	far := 0
	for i := range p {
		if dist(p[i], p[0]) > dist(p[far], p[0]) {
			far = i
		}
	}
	walk(0, far)
	if far < len(p)-1 {
		walk(far, len(p)-1)
	}
	out := make([]pt, 0, len(p))
	for i, k := range keep {
		if k {
			out = append(out, p[i])
		}
	}
	return out
}

// clipHalf keeps the part of a polygon on one side of the vertical line x=xc.
// The logo animation needs the tail to move on its own; the two halves are cut
// with a small overlap (9.0 and 9.2) so the seam stays invisible in black.
func clipHalf(p []pt, xc float64, keepLeft bool) []pt {
	side := func(q pt) bool {
		if keepLeft {
			return q.x <= xc
		}
		return q.x >= xc
	}
	out := make([]pt, 0, len(p)+4)
	for i := range p {
		cur, next := p[i], p[(i+1)%len(p)]
		if side(cur) {
			out = append(out, cur)
		}
		if side(cur) != side(next) {
			t := (xc - cur.x) / (next.x - cur.x)
			out = append(out, pt{xc, cur.y + (next.y-cur.y)*t})
		}
	}
	return out
}

func circlePts(c pt, r float64, n int) []pt {
	out := make([]pt, 0, n)
	for i := 0; i < n; i++ {
		a := 2 * math.Pi * float64(i) / float64(n)
		out = append(out, pt{c.x + r*math.Cos(a), c.y + r*math.Sin(a)})
	}
	return out
}

// capsulePts is a thick segment with round caps.
func capsulePts(a, b pt, half float64) []pt {
	d := b.sub(a)
	if d.len() == 0 {
		return circlePts(a, half, 40)
	}
	nrm := d.rot90()
	const steps = 20
	var out []pt
	for i := 0; i <= steps; i++ {
		base := math.Atan2(nrm.y, nrm.x) - math.Pi*float64(i)/steps
		out = append(out, pt{b.x + half*math.Cos(base), b.y + half*math.Sin(base)})
	}
	for i := 0; i <= steps; i++ {
		base := math.Atan2(-nrm.y, -nrm.x) - math.Pi*float64(i)/steps
		out = append(out, pt{a.x + half*math.Cos(base), a.y + half*math.Sin(base)})
	}
	return out
}

// bandPts turns a polyline into a thick band with round ends; the shell groove
// is drawn this way so it has real width in the SVG.
func bandPts(line []pt, half float64) []pt {
	const capSteps = 16
	n := len(line)
	normals := make([]pt, n)
	for i := range line {
		var t pt
		switch {
		case i == 0:
			t = line[1].sub(line[0])
		case i == n-1:
			t = line[n-1].sub(line[n-2])
		default:
			t = line[i+1].sub(line[i-1])
		}
		l := t.len()
		normals[i] = pt{-t.y / l, t.x / l}
	}
	var out []pt
	for i := range line {
		out = append(out, line[i].add(normals[i].scale(half)))
	}
	end, en := line[n-1], normals[n-1]
	base := math.Atan2(en.y, en.x)
	for i := 1; i < capSteps; i++ {
		a := base + math.Pi*float64(i)/capSteps
		out = append(out, pt{end.x + half*math.Cos(a), end.y + half*math.Sin(a)})
	}
	for i := n - 1; i >= 0; i-- {
		out = append(out, line[i].sub(normals[i].scale(half)))
	}
	start, sn := line[0], normals[0]
	base = math.Atan2(-sn.y, -sn.x)
	for i := 1; i < capSteps; i++ {
		a := base + math.Pi*float64(i)/capSteps
		out = append(out, pt{start.x + half*math.Cos(a), start.y + half*math.Sin(a)})
	}
	return out
}

func inPoly(poly []pt, p pt) bool {
	in := false
	for i, j := 0, len(poly)-1; i < len(poly); j, i = i, i+1 {
		if (poly[i].y > p.y) != (poly[j].y > p.y) {
			x := poly[i].x + (p.y-poly[i].y)/(poly[j].y-poly[i].y)*(poly[j].x-poly[i].x)
			if p.x < x {
				in = !in
			}
		}
	}
	return in
}

// ---------------------------------------------------------------- the snail

// snail is one drawing of the mark. The full and the small version are the same
// animal with different amounts of detail.
type snail struct {
	body   []pt // closed outline of tail, foot and head, 44-space
	shellC pt
	shellR float64

	spiralRMax float64 // groove starts at the rim
	spiralRMin float64 // and coils in to here
	spiralK    float64 // radius lost per radian
	groove     float64 // half width of the groove

	antA, antB pt
	antHalf    float64
	bulbC      pt
	bulbR      float64
	eyeR       float64

	slits    [][2]pt
	slitHalf []float64
}

// bodyPath is the side profile. Clockwise from the tail tip: down into the
// foot, forward along the flat sole, up the chin and the face, out to the
// snout, back over the head, down the neck, then a horizontal edge back under
// the shell to the tail. That last edge sits at the height where the shell
// still covers it, so the shell reads as sitting in front of the foot.
func bodyPath() path {
	p := path{start: pt{2.8, 30.4}}
	// Tail tip, down into the foot.
	p.curve(pt{2.4, 33.8}, pt{4.4, 37.0}, pt{9.0, 37.0})
	// The sole. Straight, because a snail's foot is flat on the ground.
	p.line(pt{31.4, 37.0})
	// The sole lifts into the chin.
	p.curve(pt{34.4, 37.0}, pt{36.4, 35.2}, pt{37.4, 32.6})
	// The face, bulging forward and a little over the foot.
	p.curve(pt{38.4, 30.8}, pt{39.6, 28.8}, pt{40.6, 26.2})
	// The snout: short, sharp, aiming up and to the right.
	p.line(pt{41.6, 21.4})
	// Back over the top of the head.
	p.line(pt{37.4, 19.6})
	// Down the back of the head into the neck, and on to the shell. The
	// point at 26.2,24.2 sits on the shell circle itself, so head and shell
	// touch and leave a notch rather than a canyon between them.
	p.curve(pt{35.2, 21.2}, pt{31.0, 22.2}, pt{26.2, 24.2})
	// The hidden edge runs back under the shell to the tail.
	p.curve(pt{22.0, 26.4}, pt{18.4, 27.6}, pt{15.0, 27.8})
	p.curve(pt{10.4, 28.0}, pt{6.6, 28.6}, pt{5.0, 29.2})
	p.curve(pt{3.8, 29.7}, pt{3.1, 30.0}, pt{2.8, 30.4})
	return p
}

func fullSnail() snail {
	return snail{
		body:       bodyPath().flatten(16),
		shellC:     pt{16.4, 18.4},
		shellR:     12.0,
		spiralRMax: 10.8,
		spiralRMin: 1.6,
		spiralK:    0.62,
		// A thin groove: at 1.0 the coil ate half the shell and the icon read
		// as a set of rings with holes between them rather than a solid shell
		// with a spiral cut into it.
		groove:  0.62,
		antA:    pt{33.6, 21.0},
		antB:    pt{31.4, 12.2},
		antHalf: 1.15,
		bulbC:   pt{31.2, 11.0},
		bulbR:   2.35,
		eyeR:    0.80,
		slits: [][2]pt{
			{{5.4, 31.8}, {9.8, 32.4}},
			{{6.2, 34.4}, {10.0, 34.6}},
		},
		slitHalf: []float64{0.34, 0.30},
	}
}

// smallSnail keeps the silhouette, a single turn of the shell groove and a
// thicker antenna, and drops the eye hole and the tail slits. At 16px the shell
// is nine pixels across; anything finer turns to mud.
func smallSnail() snail {
	return snail{
		body:       bodyPath().flatten(10),
		shellC:     pt{16.0, 18.6},
		shellR:     12.1,
		spiralRMax: 10.2,
		spiralRMin: 4.6,
		spiralK:    1.10,
		groove:     2.3,
		antA:       pt{33.2, 23.2},
		antB:       pt{34.0, 13.6},
		antHalf:    2.4,
		bulbC:      pt{34.3, 12.4},
		bulbR:      3.2,
	}
}

// spiral is the groove centreline: an Archimedean spiral from the rim inwards.
func (s snail) spiral() []pt {
	arc := (s.spiralRMax - s.spiralRMin) / s.spiralK
	const n = 1000
	out := make([]pt, 0, n)
	for i := 0; i < n; i++ {
		th := float64(i) / float64(n-1) * arc
		r := s.spiralRMax - s.spiralK*th
		// Start at the upper left of the rim so the coil reads as a spiral.
		a := th + math.Pi
		out = append(out, pt{s.shellC.x + r*math.Cos(a), s.shellC.y + r*math.Sin(a)})
	}
	return out
}

// grooveField samples the distance to the spiral centreline on a grid, so the
// rasteriser can ask "am I inside the groove?" millions of times cheaply.
type grooveField struct {
	step  float64
	n     int
	vals  []float32
	sline []pt
	snail snail
}

func newGrooveField(s snail) *grooveField {
	step := 0.05
	n := int(44.0/step) + 2
	f := &grooveField{step: step, n: n, vals: make([]float32, n*n), sline: s.spiral(), snail: s}
	for j := 0; j < n; j++ {
		y := float64(j) * step
		for i := 0; i < n; i++ {
			x := float64(i) * step
			p := pt{x, y}
			if dist(p, s.shellC) > s.shellR+2 {
				f.vals[j*n+i] = 9
				continue
			}
			best := 9.0
			for k := 1; k < len(f.sline); k++ {
				if d := segDist(p, f.sline[k-1], f.sline[k]); d < best {
					best = d
				}
			}
			f.vals[j*n+i] = float32(best)
		}
	}
	return f
}

func (f *grooveField) at(x, y float64) float64 {
	fx, fy := x/f.step, y/f.step
	i, j := int(fx), int(fy)
	if i < 0 || j < 0 || i+1 >= f.n || j+1 >= f.n {
		return 9
	}
	tx, ty := fx-float64(i), fy-float64(j)
	return float64(f.vals[j*f.n+i])*(1-tx)*(1-ty) +
		float64(f.vals[j*f.n+i+1])*tx*(1-ty) +
		float64(f.vals[(j+1)*f.n+i])*(1-tx)*ty +
		float64(f.vals[(j+1)*f.n+i+1])*tx*ty
}

// inside answers whether (x,y) is ink.
func (s snail) inside(g *grooveField, x, y float64) bool {
	p := pt{x, y}
	solid := inPoly(s.body, p) ||
		dist(p, s.shellC) <= s.shellR ||
		segDist(p, s.antA, s.antB) <= s.antHalf ||
		dist(p, s.bulbC) <= s.bulbR
	if !solid {
		return false
	}
	if g.at(x, y) <= s.groove {
		return false
	}
	if dist(p, s.bulbC) <= s.eyeR {
		return false
	}
	for i, slit := range s.slits {
		if segDist(p, slit[0], slit[1]) <= s.slitHalf[i] {
			return false
		}
	}
	return true
}

// pieces splits the mark into the solids and the holes of one drawing.
func (s snail) pieces() (solids, holes [][]pt) {
	solids = append(solids,
		s.body,
		circlePts(s.shellC, s.shellR, 240),
		capsulePts(s.antA, s.antB, s.antHalf),
		circlePts(s.bulbC, s.bulbR, 96),
	)
	holes = append(holes, bandPts(s.spiral(), s.groove))
	holes = append(holes, circlePts(s.bulbC, s.eyeR, 56))
	for i, slit := range s.slits {
		holes = append(holes, capsulePts(slit[0], slit[1], s.slitHalf[i]))
	}
	return solids, holes
}

// ---------------------------------------------------------------- raster

type raster struct {
	n    int
	vals []float32
	step float64
}

func newRaster(s snail, supersample int) *raster {
	g := newGrooveField(s)
	n := 44 * supersample
	step := 44.0 / float64(n)
	r := &raster{n: n, vals: make([]float32, n*n), step: step}
	for j := 0; j < n; j++ {
		y := (float64(j) + 0.5) * step
		for i := 0; i < n; i++ {
			x := (float64(i) + 0.5) * step
			if s.inside(g, x, y) {
				r.vals[j*n+i] = 1
			}
		}
	}
	return r
}

func (r *raster) sample(x, y float64) float64 {
	fx, fy := x/r.step-0.5, y/r.step-0.5
	i, j := int(math.Floor(fx)), int(math.Floor(fy))
	if i < 0 || j < 0 || i+1 >= r.n || j+1 >= r.n {
		return 0
	}
	tx, ty := fx-float64(i), fy-float64(j)
	v := float64(r.vals[j*r.n+i])*(1-tx)*(1-ty) +
		float64(r.vals[j*r.n+i+1])*tx*(1-ty) +
		float64(r.vals[(j+1)*r.n+i])*(1-tx)*ty +
		float64(r.vals[(j+1)*r.n+i+1])*tx*ty
	return clamp(v, 0, 1)
}

// coverage renders the mark's 44-unit box into an alpha plane of px pixels.
func (r *raster) coverage(px int) []float32 {
	out := make([]float32, px*px)
	const sub = 4
	scale := 44 / float64(px)
	for y := 0; y < px; y++ {
		for x := 0; x < px; x++ {
			var acc float64
			for sy := 0; sy < sub; sy++ {
				for sx := 0; sx < sub; sx++ {
					ux := (float64(x) + (float64(sx)+0.5)/sub) * scale
					uy := (float64(y) + (float64(sy)+0.5)/sub) * scale
					acc += r.sample(ux, uy)
				}
			}
			out[y*px+x] = float32(acc / (sub * sub))
		}
	}
	return out
}

func writeInk(p string, r *raster, px int, ink color.NRGBA) error {
	cov := r.coverage(px)
	img := image.NewNRGBA(image.Rect(0, 0, px, px))
	for i, c := range cov {
		if c <= 0.002 {
			continue
		}
		img.Pix[i*4+0] = ink.R
		img.Pix[i*4+1] = ink.G
		img.Pix[i*4+2] = ink.B
		img.Pix[i*4+3] = uint8(clamp(float64(c), 0, 1)*float64(ink.A) + 0.5)
	}
	return writePNG(p, img)
}

// writeTouchIcon paints the mark on an opaque square. iOS and the Safari
// pinned tab put the artwork on a black field when it is transparent, so this
// one has no alpha at all: paper in the back, ink in front. The mask (rounded
// corners) is applied by the system, not here.
func writeTouchIcon(p string, r *raster, px int, paper, ink color.NRGBA) error {
	img := image.NewNRGBA(image.Rect(0, 0, px, px))
	// The mark's 44-unit box fills 68% of the square, centred; iOS keeps that
	// margin visible inside its own rounded mask.
	const fill = 0.68
	margin := (1 - fill) / 2 * float64(px)
	scale := fill * float64(px) / 44 // pixels per unit
	const sub = 4
	for y := 0; y < px; y++ {
		for x := 0; x < px; x++ {
			var acc float64
			for sy := 0; sy < sub; sy++ {
				for sx := 0; sx < sub; sx++ {
					ux := (float64(x) + (float64(sx)+0.5)/sub - margin) / scale
					uy := (float64(y) + (float64(sy)+0.5)/sub - margin) / scale
					if ux < 0 || uy < 0 || ux > 44 || uy > 44 {
						continue
					}
					acc += r.sample(ux, uy)
				}
			}
			c := clamp(acc/(sub*sub), 0, 1)
			i := (y*px + x) * 4
			img.Pix[i+0] = uint8(float64(paper.R)*(1-c) + float64(ink.R)*c + 0.5)
			img.Pix[i+1] = uint8(float64(paper.G)*(1-c) + float64(ink.G)*c + 0.5)
			img.Pix[i+2] = uint8(float64(paper.B)*(1-c) + float64(ink.B)*c + 0.5)
			img.Pix[i+3] = 0xff
		}
	}
	return writePNG(p, img)
}

func writePNG(p string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// ------------------------------------------------------------------- icon

type squircle struct{ x, y, w, h, r float64 }

func (q squircle) sd(p pt) float64 {
	cx, cy := q.x+q.w/2, q.y+q.h/2
	dx := math.Abs(p.x-cx) - (q.w/2 - q.r)
	dy := math.Abs(p.y-cy) - (q.h/2 - q.r)
	ax, ay := math.Max(dx, 0), math.Max(dy, 0)
	return math.Hypot(ax, ay) + math.Min(math.Max(dx, dy), 0) - q.r
}

// inkBounds is the mark's real extent in the 44-unit art space. The geometry
// does not start at (0,0): the shell rim sits at y≈7 and the antenna bulb
// overhangs x≈42. Deriving the box from the raster instead of hard-coding it
// keeps the icon centred when the drawing changes.
func inkBounds(r *raster) (x0, y0, x1, y1 float64) {
	x0, y0, x1, y1 = 44, 44, 0, 0
	for j := 0; j < r.n; j++ {
		for i := 0; i < r.n; i++ {
			if r.vals[j*r.n+i] < 0.5 {
				continue
			}
			x := (float64(i) + 0.5) * r.step
			y := (float64(j) + 0.5) * r.step
			x0, y0 = math.Min(x0, x), math.Min(y0, y)
			x1, y1 = math.Max(x1, x), math.Max(y1, y)
		}
	}
	return
}

const iconCanvas = 1024.0

// iconFill is how much of the squircle's inner width the mark spans. Apple's
// grid leaves generous margins: at 0.72 the shell sits well inside the tile
// instead of crowding the rounded corners.
const iconFill = 0.72

// iconTransform centres the mark's bounding box inside the squircle and sizes
// it to `fill` of the squircle's width. Centring the box (not the art origin)
// is what keeps the drawing on the tile's optical centre.
func iconTransform(r *raster, fill float64) (tx, ty, scale float64) {
	x0, y0, x1, y1 := inkBounds(r)
	scale = fill * 824 / (x1 - x0)
	tx = iconCanvas/2 - ((x0+x1)/2)*scale
	ty = iconCanvas/2 - ((y0+y1)/2)*scale
	return
}

func writeIcon(p string, s snail, px int) error {
	q := squircle{x: 100, y: 100, w: 824, h: 824, r: 185}
	r := newRaster(s, 12)
	tx, ty, scale := iconTransform(r, iconFill)

	img := image.NewNRGBA(image.Rect(0, 0, px, px))
	k := iconCanvas / float64(px)
	const sub = 3
	for y := 0; y < px; y++ {
		for x := 0; x < px; x++ {
			var inside, shadow float64
			for sy := 0; sy < sub; sy++ {
				for sx := 0; sx < sub; sx++ {
					pp := pt{(float64(x) + (float64(sx)+0.5)/sub) * k,
						(float64(y) + (float64(sy)+0.5)/sub) * k}
					if q.sd(pp) <= 0 {
						inside++
					}
					if ds := q.sd(pt{pp.x, pp.y - 12}); ds > 0 {
						shadow += 0.30 * math.Exp(-(ds*ds)/(2*12*12))
					}
				}
			}
			n := float64(sub * sub)
			inside /= n
			shadow /= n
			a := 1 - (1-inside)*(1-clamp(shadow, 0, 0.34))
			if a <= 0.003 {
				continue
			}
			t := clamp((float64(y)*k-q.y)/q.h, 0, 1)
			paper := 255 - 19*t
			rgb := [3]float64{paper, paper, paper}
			mx := (float64(x) + 0.5) * k
			my := (float64(y) + 0.5) * k
			ux := (mx - tx) / scale
			uy := (my - ty) / scale
			if cov := r.sample(ux, uy); cov > 0 {
				rgb[0] += (0x16 - rgb[0]) * cov
				rgb[1] += (0x16 - rgb[1]) * cov
				rgb[2] += (0x16 - rgb[2]) * cov
			}
			i := y*img.Stride + x*4
			img.Pix[i+0] = uint8(clamp(rgb[0], 0, 255) + 0.5)
			img.Pix[i+1] = uint8(clamp(rgb[1], 0, 255) + 0.5)
			img.Pix[i+2] = uint8(clamp(rgb[2], 0, 255) + 0.5)
			img.Pix[i+3] = uint8(clamp(a, 0, 1)*255 + 0.5)
		}
	}
	return writePNG(p, img)
}

// -------------------------------------------------------------- SVG files

func logoSVG(s snail, title string) string {
	solid, holes := s.pieces()
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 44 44" role="img" aria-label="Today">
  <title>%s</title>
  <path fill="currentColor" fill-rule="nonzero" d="%s"/>
</svg>
`, title, compound(solid, holes, 3))
}

func iconSVG(s snail) string {
	solid, holes := s.pieces()
	tx, ty, scale := iconTransform(newRaster(s, 12), iconFill)
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024" role="img" aria-label="Today">
  <title>Today</title>
  <defs>
    <linearGradient id="paper" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="#ffffff"/>
      <stop offset="1" stop-color="#ececec"/>
    </linearGradient>
    <filter id="lift" x="-25%%" y="-25%%" width="150%%" height="155%%">
      <feDropShadow dx="0" dy="12" stdDeviation="14" flood-color="#000000" flood-opacity="0.16"/>
    </filter>
  </defs>
  <rect x="100" y="100" width="824" height="824" rx="185" fill="url(#paper)" filter="url(#lift)"/>
  <g transform="translate(%.3f %.3f) scale(%.5f)">
    <path fill="#161616" fill-rule="nonzero" d="%s"/>
  </g>
</svg>
`, tx, ty, scale, compound(solid, holes, 2))
}

func faviconSVG(s snail) string {
	solid, holes := s.pieces()
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 44 44" role="img" aria-label="Today">
  <title>Today</title>
  <style>
    path { fill: #1c1c1e; }
    @media (prefers-color-scheme: dark) { path { fill: #f2f2f2; } }
  </style>
  <path fill-rule="nonzero" d="%s"/>
</svg>
`, compound(solid, holes, 2))
}

// ------------------------------------------------------------------ output

// writeTrayPreview draws the 18px template glyph on a strip of menu-bar colour,
// 4× magnified, so light and dark menu bars can be checked without a screenshot.
// writeSharedLogo hands the animation the three parts as separate path data.
// The Svelte panel rotates the antenna and the tail around fixed pivots; the
// body stays in place, exactly as SKILL 8.5 describes.
func writeSharedLogo(p string, s snail) error {
	body := s.body
	outer := clipHalf(body, 9.0, false)
	tail := clipHalf(body, 9.2, true)
	mk := func(solid, holes [][]pt) string {
		return compound(solid, holes, 2)
	}
	var b strings.Builder
	b.WriteString("// Code generated by resources/gen from the same geometry as logo.svg.\n")
	b.WriteString("// Do not edit; run `go run ./resources/gen` instead.\n\n")
	b.WriteString("export const snail = {\n")
	b.WriteString("  size: 44,\n")
	fmt.Fprintf(&b, "  body: %q,\n", mk([][]pt{outer, circlePts(s.shellC, s.shellR, 240)},
		[][]pt{bandPts(s.spiral(), s.groove)}))
	fmt.Fprintf(&b, "  antenna: %q,\n", mk(
		[][]pt{capsulePts(s.antA, s.antB, s.antHalf), circlePts(s.bulbC, s.bulbR, 96)},
		[][]pt{circlePts(s.bulbC, s.eyeR, 56)}))
	fmt.Fprintf(&b, "  tail: %q,\n", mk([][]pt{tail}, nil))
	fmt.Fprintf(&b, "  antennaPivot: [%.1f, %.1f],\n", s.antA.x, s.antA.y)
	fmt.Fprintf(&b, "  tailPivot: [9.2, 33.0],\n")
	b.WriteString("} as const;\n")
	return os.WriteFile(p, []byte(b.String()), 0o644)
}

// writeTrayAlert composes the alert state: the same mark with a filled dot in
// the top-right corner. The dot sits inside the 18px canvas so the system does
// not scale it away, and it is a separate file rather than a runtime overlay so
// both states come out of one geometry.
func writeTrayAlert(p string, r *raster, px int) error {
	img := image.NewNRGBA(image.Rect(0, 0, px, px))
	// The mark keeps its full size at the small end; shrinking it here would
	// only add blur. The dot is a badge on the corner with a transparent ring
	// around it, which is what makes it read as separate ink.
	dc := pt{float64(px) * 0.80, float64(px) * 0.21}
	dr := float64(px) * 0.145
	gap := float64(px) * 0.10
	for y := 0; y < px; y++ {
		for x := 0; x < px; x++ {
			mx := (float64(x) + 0.5) / float64(px) * 44
			my := (float64(y) + 0.5) / float64(px) * 44
			cover := r.sample(mx, my)
			d := dist(pt{float64(x) + 0.5, float64(y) + 0.5}, dc)
			switch {
			case d <= dr:
				cover = 1
			case d <= dr+gap:
				cover = 0
			}
			if cover <= 0.02 {
				continue
			}
			v := 255 - int(255*math.Min(1, cover))
			img.SetNRGBA(x, y, color.NRGBA{uint8(v), uint8(v), uint8(v), 0xff})
		}
	}
	return writePNG(p, img)
}

func writeTrayPreview(p string, r *raster, bg color.NRGBA) error {
	cov := r.coverage(18)
	const scale, pad = 4, 8
	w := 18*scale + pad*2
	img := image.NewNRGBA(image.Rect(0, 0, w, w))
	for i := 0; i < w*w; i++ {
		img.Pix[i*4+0] = bg.R
		img.Pix[i*4+1] = bg.G
		img.Pix[i*4+2] = bg.B
		img.Pix[i*4+3] = 0xff
	}
	for y := 0; y < 18*scale; y++ {
		for x := 0; x < 18*scale; x++ {
			c := float64(cov[(y/scale)*18+(x/scale)])
			if c <= 0.002 {
				continue
			}
			i := (y+pad)*img.Stride + (x+pad)*4
			for k := 0; k < 3; k++ {
				img.Pix[i+k] = uint8(float64(img.Pix[i+k]) * (1 - c))
			}
		}
	}
	return writePNG(p, img)
}

// writeBootBitmap emits the 1-bit mark the board draws on its boot screen. The
// rectangle is cropped to the artwork (minus antenna tip) so the snail fills the
// space it is given; rows are top to bottom, MSB leftmost, one byte per 8 px.
func writeBootBitmap(p string, r *raster) error {
	const w, h = 48, 34
	x0, y0, x1, y1 := 2.6, 7.6, 42.2, 37.2
	stride := (w + 7) / 8
	var b strings.Builder
	b.WriteString("// Code generated by resources/gen. DO NOT EDIT.\n\n")
	b.WriteString("#pragma once\n\n#include <stdint.h>\n\n")
	b.WriteString("// The Today snail for the boot screen: 48x34, one bit per pixel,\n")
	b.WriteString("// rows top to bottom, MSB leftmost. Drawn inverted (ink = black).\n")
	b.WriteString("namespace logo {\n\n")
	fmt.Fprintf(&b, "constexpr int kWidth = %d;\nconstexpr int kHeight = %d;\nconstexpr int kStride = %d;\n\n", w, h, stride)
	b.WriteString("static const uint8_t kBits[] = {\n")
	for j := 0; j < h; j++ {
		row := make([]uint8, stride)
		for i := 0; i < w; i++ {
			ux := x0 + (float64(i)+0.5)/float64(w)*(x1-x0)
			uy := y0 + (float64(j)+0.5)/float64(h)*(y1-y0)
			if r.sample(ux, uy) >= 0.5 {
				row[i/8] |= 1 << (7 - uint(i%8))
			}
		}
		b.WriteString("    ")
		for _, v := range row {
			fmt.Fprintf(&b, "0x%02x,", v)
		}
		b.WriteString("\n")
	}
	b.WriteString("};\n\n}  // namespace logo\n")
	return os.WriteFile(p, []byte(b.String()), 0o644)
}

// asciiBlock prints the rasterised mark at a pixel grid, which is the only way
// to see what a 16px icon actually looks like without opening a PNG. Rows are
// doubled up because terminal cells are about twice as tall as they are wide.
func asciiBlock(s snail, px int) {
	cov := newRaster(s, 8).coverage(px)
	ramp := []string{" ", ".", "+", "#"}
	for y := 0; y < px; y += 2 {
		var b strings.Builder
		for x := 0; x < px; x++ {
			// Average the pair of rows that share one terminal line.
			c := float64(cov[y*px+x])
			if y+1 < px {
				c = (c + float64(cov[(y+1)*px+x])) / 2
			}
			switch {
			case c > 0.66:
				b.WriteString(ramp[3])
			case c > 0.33:
				b.WriteString(ramp[2])
			case c > 0.06:
				b.WriteString(ramp[1])
			default:
				b.WriteString(ramp[0])
			}
		}
		fmt.Println(strings.TrimRight(b.String(), " "))
	}
}

// asciiPreview prints the mark as characters so the shape can be checked in a
// terminal, where PNGs are useless. Invoked with -ascii.
func asciiPreview(s snail, cols int) {
	r := newRaster(s, 6)
	const y0, y1 = 6.0, 37.0
	rows := int(float64(cols) * (y1 - y0) / 44 / 2)
	for j := 0; j < rows; j++ {
		var b strings.Builder
		for i := 0; i < cols; i++ {
			x := (float64(i) + 0.5) / float64(cols) * 44
			y := y0 + (float64(j)+0.5)/float64(rows)*(y1-y0)
			switch c := r.sample(x, y); {
			case c > 0.6:
				b.WriteString("#")
			case c > 0.25:
				b.WriteString("+")
			case c > 0.05:
				b.WriteString(".")
			default:
				b.WriteString(" ")
			}
		}
		fmt.Println(strings.TrimRight(b.String(), " "))
	}
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-ascii" {
		asciiPreview(fullSnail(), 88)
		fmt.Println("--- 16px ---")
		for _, px := range []int{16, 18, 24, 32} {
			fmt.Printf("--- %dpx ---\n", px)
			asciiBlock(smallSnail(), px)
		}
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}

func run() error {
	if _, err := os.Stat("resources"); err != nil {
		return fmt.Errorf("run from the repository root: resources/ not found")
	}
	full, small := fullSnail(), smallSnail()

	files := map[string]string{
		"resources/logo.svg":       logoSVG(full, "Today"),
		"resources/logo-small.svg": logoSVG(small, "Today"),
		"resources/icon.svg":       iconSVG(full),
		"resources/tray.svg":       logoSVG(small, "Today"),
		"resources/favicon.svg":    faviconSVG(small),
	}
	for name, body := range files {
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			return err
		}
	}

	fullRaster := newRaster(full, 12)
	smallRaster := newRaster(small, 16)

	ink := color.NRGBA{0x16, 0x16, 0x16, 0xff}
	favLight := color.NRGBA{0x1c, 0x1c, 0x1e, 0xff}
	favDark := color.NRGBA{0xf2, 0xf2, 0xf2, 0xff}
	black := color.NRGBA{0, 0, 0, 0xff}
	paper := color.NRGBA{0xf4, 0xf3, 0xee, 0xff}

	rasters := map[string]struct {
		r   *raster
		px  int
		ink color.NRGBA
	}{
		"resources/tray.png":          {smallRaster, 18, black},
		"resources/tray@2x.png":       {smallRaster, 36, black},
		"resources/favicon-light.png": {smallRaster, 32, favLight},
		"resources/favicon-dark.png":  {smallRaster, 32, favDark},
	}
	for name, job := range rasters {
		if err := writeInk(name, job.r, job.px, job.ink); err != nil {
			return err
		}
	}
	if err := writeIcon("resources/icon.png", full, 1024); err != nil {
		return err
	}
	if err := writeTouchIcon("resources/apple-touch-icon.png", smallRaster, 180,
		paper, ink); err != nil {
		return err
	}
	// Two menu-bar states: the plain mark, and the alert mark with a dot.
	if err := writeTrayAlert("resources/tray-alert.png", smallRaster, 18); err != nil {
		return err
	}
	if err := writeTrayAlert("resources/tray-alert@2x.png", smallRaster, 36); err != nil {
		return err
	}
	// The animated mark lives in the shared package so the web app can use it
	// too; the desktop panel is where it actually plays.
	if err := writeSharedLogo("frontend/shared/src/snail.ts", small); err != nil {
		return err
	}

	dir := "/tmp/todayui/hw13"
	previews := map[string]struct {
		r   *raster
		px  int
		ink color.NRGBA
	}{
		filepath.Join(dir, "logo-16.png"):       {smallRaster, 16, ink},
		filepath.Join(dir, "logo-32.png"):       {smallRaster, 32, ink},
		filepath.Join(dir, "logo-128.png"):      {fullRaster, 128, ink},
		filepath.Join(dir, "logo-512.png"):      {fullRaster, 512, ink},
		filepath.Join(dir, "logo-small-16.png"): {smallRaster, 16, ink},
		filepath.Join(dir, "logo-small-24.png"): {smallRaster, 24, ink},
	}
	for name, job := range previews {
		if err := writeInk(name, job.r, job.px, job.ink); err != nil {
			return err
		}
	}
	if err := writeIcon(filepath.Join(dir, "icon-512.png"), full, 512); err != nil {
		return err
	}
	if err := writeIcon(filepath.Join(dir, "icon-1024.png"), full, 1024); err != nil {
		return err
	}
	if err := writeTrayPreview(filepath.Join(dir, "tray-light.png"), smallRaster,
		color.NRGBA{0xf4, 0xf3, 0xee, 0xff}); err != nil {
		return err
	}
	if err := writeTrayPreview(filepath.Join(dir, "tray-dark.png"), smallRaster,
		color.NRGBA{0x1a, 0x1a, 0x1e, 0xff}); err != nil {
		return err
	}
	if err := writeBootBitmap("firmware/src/logo_inc.h", smallRaster); err != nil {
		return err
	}

	// The web pages and the tray load icons from their own package directories
	// (go:embed cannot reach outside a package), so keep those copies in step.
	copies := [][2]string{
		{"resources/tray.png", "internal/host/assets/tray.png"},
		{"resources/tray@2x.png", "internal/host/assets/tray@2x.png"},
		{"resources/tray-alert.png", "internal/host/assets/tray-alert.png"},
		{"resources/tray-alert@2x.png", "internal/host/assets/tray-alert@2x.png"},
		{"resources/favicon-light.png", "internal/server/webapp/dist/favicon-light.png"},
		{"resources/favicon-dark.png", "internal/server/webapp/dist/favicon-dark.png"},
		{"resources/apple-touch-icon.png", "internal/server/webapp/dist/apple-touch-icon.png"},
	}
	for _, c := range copies {
		body, err := os.ReadFile(c[0])
		if err != nil {
			return err
		}
		if err := os.WriteFile(c[1], body, 0o644); err != nil {
			return err
		}
	}
	if body, err := os.ReadFile("resources/apple-touch-icon.png"); err == nil {
		_ = os.WriteFile("frontend/web/public/apple-touch-icon.png", body, 0o644)
	}
	if body, err := os.ReadFile("resources/favicon-light.png"); err == nil {
		_ = os.WriteFile("frontend/web/public/favicon-light.png", body, 0o644)
	}
	if body, err := os.ReadFile("resources/favicon-dark.png"); err == nil {
		_ = os.WriteFile("frontend/web/public/favicon-dark.png", body, 0o644)
	}

	report(full, small, fullRaster, smallRaster)
	return nil
}

func report(full, small snail, fr, sr *raster) {
	inkShare := func(r *raster) float64 {
		var on float64
		for _, v := range r.vals {
			on += float64(v)
		}
		return on / float64(len(r.vals)) * 100
	}
	fmt.Printf("logo: ink %.1f%% (full), %.1f%% (small) of the 44×44 box\n",
		inkShare(fr), inkShare(sr))
	for _, job := range []struct {
		name string
		s    snail
	}{{"full", full}, {"small", small}} {
		solid, holes := job.s.pieces()
		minX, minY, maxX, maxY := 99.0, 99.0, -99.0, -99.0
		for _, poly := range solid {
			for _, q := range poly {
				minX, maxX = math.Min(minX, q.x), math.Max(maxX, q.x)
				minY, maxY = math.Min(minY, q.y), math.Max(maxY, q.y)
			}
		}
		fmt.Printf("%s: %d solids, %d holes, bbox x %.1f..%.1f y %.1f..%.1f (44 grid)\n",
			job.name, len(solid), len(holes), minX, maxX, minY, maxY)
	}
	fmt.Println("previews: /tmp/todayui/hw13/")
}
