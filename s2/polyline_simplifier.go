// Copyright 2023 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS-IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package s2

import (
	"math"

	"github.com/golang/geo/r3"
	"github.com/golang/geo/s1"
)

// Author: ericv@google.com (Eric Veach)
// Author: roberts@google.com (Robert Snedegar)
//
// This is a helper tool for simplifying polylines.  It allows you to compute
// a maximal edge that intersects a sequence of discs, and that optionally
// avoids a different sequence of discs. The results are conservative in that
// the edge is guaranteed to intersect or avoid the specified discs using
// exact arithmetic (see s2predicates).
//
// Note that Builder can also simplify polylines and supports more features
// (e.g., snapping to CellID centers), so it is only recommended to use this
// type if Builder does not meet your needs.
//
// Here is a simple example showing how to simplify a polyline into a sequence
// of edges that stay within "maxError" of the original edges:
//
//	v := []Point{ ... }
//	simplifier := NewPolylineSimplifier(v[0])
//	for i := 1; i < len(v); i++ {
//		if !simplifier.Extend(v[i]) {
//			emitEdge(simplifier.Src(), v[i-1])
//			simplifier = newPolylineSimplifier(v[i-1])
//		}
//		simplifier.TargetDisc(v[i], maxError)
//	}
//	emitEdge(simplifer.Src(), v[len(v)-1])
//
// Note that the points targeted by TargetDisc do not need to be the same as
// the candidate endpoints passed to Extend.  So for example, you could target
// the original vertices of a polyline, but only consider endpoints that are
// snapped to E7 coordinates or CellID centers.
//
// Please be aware that this tool works by maintaining a range of acceptable
// angles (bearings) from the start vertex to the hypothetical destination
// vertex.  It does not keep track of distances to any of the discs to be
// targeted or avoided.  Therefore to use this type correctly, constraints
// should be added in increasing order of distance.  (The actual requirement
// is slightly weaker than this, which is why it is not enforced, but
// basically you should only call TargetDisc() and AvoidDisc() with arguments
// that you want to constrain the immediately following call to Extend().)
type PolylineSimplifier struct {
	src        Point       // Output edge source vertex.
	xDir, yDir Point       // Orthonormal frame for mapping vectors to angles.
	window     s1.Interval // Allowable range of angles for the output edge.

	// We store the discs to avoid individually until TargetDisc() is first
	// called with a disc that does not contain the source vertex.  At that time
	// all such discs are processed by using them to constrain "window", and
	// this vector is cleared.
	rangesToAvoid []rangeToAvoid
}

// rangeToAvoid holds the edge and side of the disc being processed.
//
// Unfortunately, the discs to avoid cannot be processed until the direction
// of the output edge is constrained to lie within an S1Interval of at most
// 180 degrees.  This happens only when the first target disc is added that
// does not contain the source vertex.  Until that time we simply store all
// the discs as ranges of directions to avoid.
type rangeToAvoid struct {
	interval s1.Interval // Range of directions to avoid.
	onLeft   bool        // Is this disc to the left of the output edge?
}

// NewPolylineSimplifier starts a new simplifier with edge at "src".
func NewPolylineSimplifier(src Point) *PolylineSimplifier {
	p := &PolylineSimplifier{
		src:           src,
		window:        s1.FullInterval(),
		rangesToAvoid: []rangeToAvoid{},
	}

	// Precompute basis vectors for the tangent space at "src".  This is similar
	// to Frame() except that we don't normalize the vectors.  As it turns
	// out, the two basis vectors below have the same magnitude (up to the
	// length error in Normalize).

	// Find the index of the component whose magnitude is smallest.
	// We define the "y" basis vector as the cross product of "src" and the
	// basis vector for axis "i".  Let "j" and "k" be the indices of the other
	// two components in cyclic order.
	//
	// Compute the cross product of "yDir" and "src". We write out the cross
	// product here mainly for documentation purposes; it also happens to save a
	// few multiplies because unfortunately the optimizer does *not* get rid of
	// multiplies by zero (since these multiplies propagate NaN, for example).
	//
	// C++ has operator[] access to Vector components which we do not, so we have
	// to unroll that here to get the equivalent results.
	c := src.SmallestComponent()
	if c == r3.XAxis {
		// i = 0, j = 1, k = 2
		p.yDir.X = 0
		p.yDir.Y = src.Z
		p.yDir.Z = -src.Y

		p.xDir.X = src.Y*src.Y + src.Z*src.Z
		p.xDir.Y = -src.Y * src.X
		p.xDir.Z = -src.Z * src.X
	} else if c == r3.YAxis {
		// i = 2, j = 0, k = 1
		p.yDir.Z = 0
		p.yDir.X = src.Y
		p.yDir.Y = -src.X

		p.xDir.Z = src.X*src.X + src.Y*src.Y
		p.xDir.X = -src.X * src.Z
		p.xDir.Y = -src.Y * src.Z
	} else { // ZAxis is smallest
		// i = 1, j = 2, k = 0
		p.yDir.Y = 0
		p.yDir.Z = src.X
		p.yDir.X = -src.Z

		p.xDir.Y = src.Z*src.Z + src.X*src.X
		p.xDir.Z = -src.Z * src.Y
		p.xDir.X = -src.X * src.Y
	}

	return p
}

// Src returns the source vertex of the output edge.
func (p *PolylineSimplifier) Src() Point {
	return p.src
}

// TargetDisc reports if it is possible to intersect the target disc given previous
// constraints.
// Requires that the output edge must pass through the given disc.
func (p *PolylineSimplifier) TargetDisc(pt Point, r s1.ChordAngle) bool {
	// Shrink the target interval by the maximum error from all sources.  This
	// guarantees that the output edge will intersect the given disc.
	semiwidth := p.semiwidth(pt, r, -1) //round down
	if semiwidth >= math.Pi {
		// The target disc contains "src", so there is nothing to do.
		return true
	}
	if semiwidth < 0 {
		p.window = s1.EmptyInterval()
		return false
	}
	// Otherwise compute the angle interval corresponding to the target disc and
	// intersect it with the current window.
	center := p.direction(pt)
	target := s1.IntervalFromEndpoints(center, center).Expanded(semiwidth)
	p.window = p.window.Intersection(target)

	// If there are any angle ranges to avoid, they can be processed now.
	for _, r := range p.rangesToAvoid {
		p.avoidRange(r.interval, r.onLeft)
	}

	p.rangesToAvoid = []rangeToAvoid{}
	return !p.window.IsEmpty()
}

func (p *PolylineSimplifier) direction(pt Point) float64 {
	return math.Atan2(pt.Dot(p.yDir.Vector), pt.Dot(p.xDir.Vector))
}

// semiwidth Computes half the angle in radians subtended from the source vertex by a
// disc of radius "r" centered at "p", rounding the result conservatively up
// or down according to whether roundDirection is +1 or -1.  (So for example,
// if roundDirection == +1 then the return value is an upper bound on the
// true result.)
func (p *PolylineSimplifier) semiwidth(pt Point, r s1.ChordAngle, roundDirection float64) float64 {
	const dblError = 0.5 * dblEpsilon

	// Using spherical trigonometry,
	//
	//   sin(semiwidth) = sin(r) / sin(a)
	//
	// where "a" is the angle between "src" and "p".  Rather than measuring
	// these angles, instead we measure the squared chord lengths through the
	// interior of the sphere (i.e., Cartersian distance).  Letting "r2" be the
	// squared chord distance corresponding to "r", and "a2" be the squared
	// chord distance corresponding to "a", we use the relationships
	//
	//    sin^2(r) = r2 (1 - r2 / 4)
	//    sin^2(a) = d2 (1 - d2 / 4)
	//
	// which follow from the fact that r2 = (2 * sin(r / 2)) ^ 2, etc.

	// "a2" has a relative error up to 5 * dblError, plus an absolute error of up
	// to 64 * dblError^2 (because "src" and "p" may differ from unit length by
	// up to 4 * dblError).  We can correct for the relative error later, but for
	// the absolute error we use "roundDirection" to account for it now.
	r2 := float64(r)
	a2 := float64(ChordAngleBetweenPoints(p.src, pt))
	a2 -= 64 * dblError * dblError * roundDirection
	if a2 <= r2 {
		return math.Pi // The given disc contains "src".
	}

	sin2R := r2 * (1 - 0.25*r2)
	sin2A := a2 * (1 - 0.25*a2)
	semiwidth := math.Asin(math.Sqrt(sin2R / sin2A))

	// We compute bounds on the errors from all sources:
	//
	//   - The call to semiwidth (this call).
	//   - The call to direction that computes the center of the interval.
	//   - The call to direction in Extend that tests whether a given point
	//     is an acceptable destination vertex.
	//
	// Summary of the errors in GetDirection:
	//
	// yDir has no error.
	//
	// xDir has a relative error of dblError in two components, a relative
	// error of 2 * dblError in the other component, plus an overall relative
	// length error of 4 * dblError (compared to yDir) because "src" is assumed
	// to be normalized only to within the tolerances of Normalize().
	//
	// p.DotProd(yDir) has a relative error of 1.5 * dblError and an
	// absolute error of 1.5 * dblError * yDir.Norm().
	//
	// p.DotProd(xDir) has a relative error of 5.5 * dblError and an absolute
	// error of 3.5 * dblError * yDir.Norm() (noting that xDir and yDir
	// have the same length to within a relative error of 4 * dblError).
	//
	// It's possible to show by taking derivatives that these errors can affect
	// the angle atan2(y, x) by up 7.093 * dblError radians.  Rounding up and
	// including the call to atan2 gives a final error bound of 10 * dblError.
	//
	// Summary of the errors in GetSemiwidth:
	//
	// The distance a2 has a relative error of 5 * dblError plus an absolute
	// error of 64 * dblError^2 because the points "src" and "p" may differ from
	// unit length (by up to 4 * dblError).  We have already accounted for the
	// absolute error above, leaving only the relative error.
	//
	// sin2R has a relative error of 2 * dblError.
	//
	// sin2A has a relative error of 12 * dblError assuming that a2 <= 2,
	// i.e. distance(src, p) <= 90 degrees.  (The relative error gets
	// arbitrarily larger as this distance approaches 180 degrees.)
	//
	// semiwidth has a relative error of 17 * dblError.
	//
	// Finally, (center +/- semiwidth) has a rounding error of up to 4 * dblError
	// because in theory, the result magnitude may be as large as 1.5 * math.Pi
	// which is larger than 4.0.  This gives a total error of:
	err := (2*10+4)*dblError + 17*dblError*semiwidth

	return semiwidth + roundDirection*err
}

func (p *PolylineSimplifier) avoidRange(avoidInterval s1.Interval, discOnLeft bool) {
	// If "avoidInterval" is a proper subset of "window", then in theory the
	// result should be two intervals.  One interval points towards the given
	// disc and passes on the correct side of it, while the other interval points
	// away from the disc.  However the latter interval never contains an
	// acceptable output edge direction (as long as this type is being used
	// correctly) and can be safely ignored.  This is true because (1) "window"
	// is not full, which means that it contains at least one vertex of the input
	// polyline and is at most 180 degrees in length, and (2) "discOnLeft" is
	// computed with respect to the next edge of the input polyline, which means
	// that the next input vertex is either inside "avoidInterval" or somewhere
	// in the 180 degrees to its right/left according to "discOnLeft", which
	// means that it cannot be contained by the subinterval that we ignore.
	if p.window.ContainsInterval(avoidInterval) {
		if discOnLeft {
			p.window = s1.IntervalFromEndpoints(p.window.Lo, avoidInterval.Lo)
		} else {
			p.window = s1.IntervalFromEndpoints(avoidInterval.Hi, p.window.Hi)
		}
	} else {
		p.window = p.window.Intersection(avoidInterval.Complement())
	}

}

// CanAvoidDisc reports if the disc can be avoided given previous constraints, or if
// the discs to avoid have not been processed yet. Returns false if the disc
// cannot be avoided.
//
// Requires that the output edge must avoid the given disc.  "discOnLeft"
// specifies whether the disc must be to the left or right of the output
// edge AB.  (This feature allows the simplified edge to preserve the
// topology of the original polyline with respect to other nearby points.)
//
// More precisely, let AB be the output edge, P be the center of the disc,
// and r be its radius.  Then this method ensures that
//
//	(1) Distance(AB, P) > r, and
//	(2) if DotProd(AB, AP) > 0, then Sign(ABP) > 0 iff discOnLeft is true.
//
// The second condition says that "discOnLeft" has an effect if and only
// if P is not behind the source vertex A with respect to the direction AB.
//
// If your input is a polyline, you can compute "discOnLeft" as follows.
// Let the polyline be ABCDE and assume that it already avoids a set of
// points X_i.  Suppose that you have aleady added ABC to the simplifier, and
// now want to extend the edge chain to D.  First find the X_i that are near
// the edge CD, then discard the ones such that AX_i <= AC or AX_i >= AD
// (since these points have either already been considered or aren't
// relevant yet).  Now X_i is to the left of the polyline if and only if
// OrderedCCW(A, D, X_i, C) (in other words, if X_i is to the left of
// the angle wedge ACD).  Note that simply testing Sign(C, D, X_i)
// or Sign(A, D, X_i) does not handle all cases correctly.
func (p *PolylineSimplifier) CanAvoidDisc(point Point, radius s1.ChordAngle, discOnLeft bool) bool {
	// Expand the interval by the maximum error from all sources.  This
	// guarantees that the final output edge will avoid the given disc.
	semiwidth := p.semiwidth(point, radius, 1) // round up
	if semiwidth >= math.Pi {
		// The disc to avoid contains "src", so it can't be avoided.
		p.window = s1.EmptyInterval()
		return false
	}
	// Compute the disallowed range of angles: the angle subtended by the disc
	// on one side, and 90 degrees on the other (to satisfy "disc_on_left").
	center := p.direction(point)
	var dLeft, dRight float64
	if discOnLeft {
		dLeft = math.Pi / 2.0
		dRight = semiwidth
	} else {
		dLeft = semiwidth
		dRight = math.Pi / 2.0
	}
	avoidInterval := s1.IntervalFromEndpoints(math.Remainder(center-dRight, 2*math.Pi),
		math.Remainder(center+dLeft, 2*math.Pi))

	if p.window.IsFull() {
		// Discs to avoid can't be processed until window is reduced to at most
		// 180 degrees by a call to TargetDisc().  Save it for later.
		p.rangesToAvoid = append(p.rangesToAvoid, rangeToAvoid{avoidInterval, discOnLeft})
		return true
	}
	p.avoidRange(avoidInterval, discOnLeft)
	return !p.window.IsEmpty()

}

// CanExtend reports if the edge (src, dst) satisfies all of the targeting
// requirements so far. Returns false if the edge would be longer than
// 90 degrees (such edges are not supported).
func (p *PolylineSimplifier) CanExtend(dst Point) bool {
	// We limit the maximum edge length to 90 degrees in order to simplify the
	// error bounds.  (The error gets arbitrarily large as the edge length
	// approaches 180 degrees.)
	if ChordAngleBetweenPoints(p.src, dst) > s1.RightChordAngle {
		return false
	}

	// Otherwise check whether this vertex is in the acceptable angle range.
	dir := p.direction(dst)
	if !p.window.Contains(dir) {
		return false
	}

	// Also check any angles ranges to avoid that have not been processed yet.
	for _, r := range p.rangesToAvoid {
		if r.interval.Contains(dir) {
			return false
		}
	}
	return true
}
