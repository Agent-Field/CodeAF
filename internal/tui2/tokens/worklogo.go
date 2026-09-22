package tokens

import "math"

// WorkLogoCount is the shared number of running logo studies.
const WorkLogoCount = 10

// WorkLogoWidth and WorkLogoHeight keep every study in the same compact slot.
const WorkLogoWidth, WorkLogoHeight = 10, 4

// WorkLogoHalfBlock is raster material rather than a semantic status icon.
const WorkLogoHalfBlock = "▀"
const WorkLogoLowerBlock = "▄"

// WorkLogoInk carries coverage independently of a terminal's current palette.
type WorkLogoInk struct{ Ink, Gold float64 }

// WorkLogoCell contains the two square pixels in one terminal character cell.
type WorkLogoCell struct{ Top, Bottom WorkLogoInk }

type point struct{ x, y float64 }

// WorkLogo rasterizes a running study into a fixed-size terminal mark. The caller
// supplies elapsed seconds, so rendering never owns a timer or random source.
func WorkLogo(style int, seconds float64) [WorkLogoHeight][WorkLogoWidth]WorkLogoCell {
	if style < 0 || style >= WorkLogoCount {
		style = 0
	}
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
		seconds = 0
	}
	scene := motionScene(seconds, style)
	var frame [WorkLogoHeight][WorkLogoWidth]WorkLogoCell
	for row := range frame {
		for col := range frame[row] {
			halves := [2]WorkLogoInk{}
			for half := 0; half < 2; half++ {
				for sy := 0; sy < 3; sy++ {
					for sx := 0; sx < 3; sx++ {
						x := 15 + (float64(col)+(float64(sx)+.5)/3)*70/WorkLogoWidth
						y := 20 + (float64(row)+(float64(half)+(float64(sy)+.5)/3)/2)*60/WorkLogoHeight
						c := scene.sample(x, y)
						halves[half].Ink += c.Ink / 9
						halves[half].Gold += c.Gold / 9
					}
				}
			}
			frame[row][col] = WorkLogoCell{halves[0], halves[1]}
		}
	}
	return frame
}

// Geometry is authored once per frame, not once per subpixel. Each frame is
// sampled from elapsed time, so delayed terminal writes never change the tempo.
type primitive struct {
	kind          int
	a, b          point
	rx, ry, alpha float64
	gold          bool
}
type scene struct {
	items []primitive
}

func (s *scene) dot(x, y, rx, ry, alpha float64) {
	s.items = append(s.items, primitive{a: point{x, y}, rx: rx, ry: ry, alpha: alpha, gold: true})
}
func (s *scene) line(a, b point, r, alpha float64, gold bool) {
	s.items = append(s.items, primitive{kind: 1, a: a, b: b, rx: r, alpha: alpha, gold: gold})
}
func (s *scene) ring(x, y, r, alpha float64) {
	s.items = append(s.items, primitive{kind: 2, a: point{x, y}, rx: r, ry: .7, alpha: alpha, gold: true})
}
func (s *scene) chevron(cx, cy, scale, angle, flat float64) {
	// flat=1 is a straight paddle, flat=0 is >, flat=2 flexes into <.
	pts := []point{{-12 * (1 - flat), -19.9}, {10.2 * (1 - flat), 0}, {-12 * (1 - flat), 19.9}}
	for i := range pts {
		x, y := pts[i].x*scale, pts[i].y*scale
		pts[i] = point{cx + x*math.Cos(angle) - y*math.Sin(angle), cy + x*math.Sin(angle) + y*math.Cos(angle)}
	}
	s.line(pts[0], pts[1], 4.4*scale, 1, false)
	s.line(pts[1], pts[2], 4.4*scale, 1, false)
}
func distanceSegment(x, y float64, a, b point) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	den := dx*dx + dy*dy
	u := 0.0
	if den > 0 {
		u = math.Max(0, math.Min(1, ((x-a.x)*dx+(y-a.y)*dy)/den))
	}
	return math.Hypot(x-a.x-u*dx, y-a.y-u*dy)
}
func (s scene) sample(x, y float64) WorkLogoInk {
	c := WorkLogoInk{}
	for _, p := range s.items {
		hit := false
		switch p.kind {
		case 0:
			dx, dy := (x-p.a.x)/p.rx, (y-p.a.y)/p.ry
			hit = dx*dx+dy*dy < 1
		case 1:
			hit = distanceSegment(x, y, p.a, p.b) < p.rx
		case 2:
			hit = math.Abs(math.Hypot(x-p.a.x, y-p.a.y)-p.rx) < p.ry
		}
		if hit {
			c.Ink *= 1 - p.alpha
			c.Gold *= 1 - p.alpha
			if p.gold {
				c.Gold += p.alpha
			} else {
				c.Ink += p.alpha
			}
		}
	}
	return c
}

// Cubic Bézier easing uses the motion skill's strong ease-in-out / ease-out
// curves. Solve x first; treating t as Bézier x would distort the timing.
func bezier(x, x1, y1, x2, y2 float64) float64 {
	lo, hi := 0.0, 1.0
	for i := 0; i < 18; i++ {
		u := (lo + hi) / 2
		v := 1 - u
		bx := 3*v*v*u*x1 + 3*v*u*u*x2 + u*u*u
		if bx < x {
			lo = u
		} else {
			hi = u
		}
	}
	u := (lo + hi) / 2
	v := 1 - u
	return 3*v*v*u*y1 + 3*v*u*u*y2 + u*u*u
}
func ease(x float64) float64    { return bezier(math.Max(0, math.Min(1, x)), .77, 0, .175, 1) }
func easeOut(x float64) float64 { return bezier(math.Max(0, math.Min(1, x)), .23, 1, .32, 1) }
func motionScene(t float64, style int) scene {
	s := scene{}

	p := math.Mod(t, 2.8) / 2.8
	a := 2 * math.Pi * p
	travel := (1 - math.Cos(a)) / 2
	impact := math.Pow((1+math.Cos(a))/2, 10)
	switch style {
	case 0: // Contact point moves with the straightening paddle.
		flat := impact
		s.chevron(37.5-1.5*impact, 50, 1, 0, flat)
		tip := 37.5 - 1.5*impact + 10.2*(1-flat)
		rx := 6.5 - 1.8*impact
		ry := 6.5 + 1.8*impact
		s.dot(tip+4.4+rx+19*travel, 50-3*math.Sin(a)*math.Sin(a), rx, ry, 1)
	case 1:
		left := math.Pow(1-travel, 10)
		right := math.Pow(travel, 10)
		s.chevron(26, 50, .72, 0, left)
		s.chevron(74, 50, .72, math.Pi, right)
		lx := 26 + 7.344*(1-left) + 3.168 + 4.5
		rx := 74 - 7.344*(1-right) - 3.168 - 4.5
		s.dot(lx+(rx-lx)*travel, 50-6*math.Sin(a)*math.Sin(a), 4.5-1.1*(left+right), 4.5+1.1*(left+right), 1)
	case 2:
		s.chevron(50, 66, .82, -math.Pi/2, impact)
		r := 6.5 + 1.5*impact
		tip := 66 - 8.364*(1-impact)
		s.dot(50, tip-3.608-(6.5-1.5*impact)-20*travel, r, 6.5-1.5*impact, 1)
	case 3:
		// A slow load, quick launch, then a gentle return, with no loop teleport.
		pull, flight := 0.0, 0.0
		if p < .28 {
			pull = ease(p / .28)
		} else if p < .44 {
			u := (p - .28) / .16
			pull = 1 - easeOut(u)
			flight = easeOut(u)
		} else {
			flight = 1 - ease((p-.44)/.56)
		}
		s.chevron(37.5, 50, 1, 0, 1.5*pull)
		x := 59 - 15*pull + 18*flight
		y := 50 - 10*math.Sin(math.Pi*flight)
		if p > .28 && p < .55 {
			for i := 3; i > 0; i-- {
				s.dot(x-float64(i)*3, y, 2.8, 2.8, .12*float64(4-i)*math.Pow(math.Sin(math.Pi*(p-.28)/.27), 2))
			}
		}
		s.dot(x, y, 6.4+1.2*pull, 6.4-1.2*pull, 1)
	case 4:
		s.chevron(49, 65, .58, -math.Pi/2+.16*math.Sin(a), .35)
		for i := 0; i < 3; i++ {
			q := a + float64(i)*2*math.Pi/3
			x := 50 + 22*math.Cos(q)
			y := 43 - 14*math.Sin(q)
			r := 4.5 + .8*math.Sin(q)
			s.dot(x, y, r, r, .85+.15*math.Sin(q))
		}
	case 5:
		// Ease the roll to give the mark a moment upright between turns.
		rotation := 2 * math.Pi * ease(p)
		s.chevron(38, 50, .82, rotation, 0)
		s.dot(68+6*math.Sin(a), 50-14*math.Sin(a), 6.5, 6.5, 1)
	case 6:
		s.chevron(30, 50, .58, 0, .16*math.Sin(a))
		left := math.Pow(math.Max(0, math.Sin(a)), 2)
		right := math.Pow(math.Max(0, -math.Sin(a)), 2)
		s.dot(54-8*left, 50-12*left, 4.5, 4.5, 1)
		s.dot(63, 50, 4.5, 4.5, 1)
		s.dot(72+8*right, 50-12*right, 4.5, 4.5, 1)
	case 7:
		s.chevron(37.5, 50, 1-.035*math.Sin(a), 0, .10*(1+math.Sin(a)))
		for i := 0; i < 2; i++ {
			q := math.Mod(p+float64(i)*.5, 1)
			s.ring(68, 50, 6+9*q, .65*math.Sin(math.Pi*q)*math.Sin(math.Pi*q))
		}
		r := 6.5 + .9*math.Sin(a)
		s.dot(68, 50, r, r, 1)
	case 8:
		for i := 0; i < 3; i++ {
			q := a - float64(i)*.7
			s.chevron(27+float64(i)*12, 50, .43, 0, .65*(1+math.Sin(q)))
		}
		squeeze := (1 + math.Sin(a-2.1)) / 2
		s.dot(67+7*squeeze, 50, 5.5-1.1*squeeze, 5.5+1.1*squeeze, 1)
	case 9:
		// Draw the far half behind the chevron and the near half in front.
		ball := func() {
			for i := 4; i >= 0; i-- {
				q := a - float64(i)*.09
				r := 4.5 - float64(i)*.45
				alpha := 1 - float64(i)*.18
				s.dot(50+23*math.Cos(q), 50+14*math.Sin(2*q), r, r, alpha)
			}
		}
		if math.Sin(a) < 0 {
			ball()
		}
		s.chevron(39, 50, .80, .10*math.Sin(a), 0)
		if math.Sin(a) >= 0 {
			ball()
		}
	}
	return s
}
