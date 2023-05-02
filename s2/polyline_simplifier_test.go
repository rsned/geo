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
	"testing"

	"github.com/golang/geo/s1"
)

func TestPolylineSimplifier(t *testing.T) {
	tests := []struct {
		src           string
		dest          string
		target        string
		avoid         string
		discOnLeft    []bool
		radiusDegrees s1.Angle
		want          bool
	}{
		// No constraints.
		{
			// No constraints, dst == src.
			src:           "0:1",
			dest:          "0:1",
			target:        "",
			avoid:         "",
			discOnLeft:    nil,
			radiusDegrees: 0,
			want:          true,
		},
		{
			// No constraints, dst != src.
			src:           "0:1",
			dest:          "1:0",
			target:        "",
			avoid:         "",
			discOnLeft:    nil,
			radiusDegrees: 0,
			want:          true,
		},
		{
			// No constraints, (src, dst) longer than 90 degrees (not supported).
			src:           "0:0",
			dest:          "0:91",
			target:        "",
			avoid:         "",
			discOnLeft:    nil,
			radiusDegrees: 0,
			want:          false,
		},
		// Target one point.
		{
			// Three points on a straight line.  In theory zero tolerance should work,
			// but in practice there are floating point errors.
			src:           "0:0",
			dest:          "0:2",
			target:        "0:1",
			avoid:         "",
			discOnLeft:    nil,
			radiusDegrees: s1.Angle(1e-10) * s1.Degree,
			want:          true,
		},
		{
			// Three points where the middle point is too far away.
			src:           "0:0",
			dest:          "0:2",
			target:        "1:1",
			avoid:         "",
			discOnLeft:    nil,
			radiusDegrees: s1.Angle(0.9) * s1.Degree,
			want:          false,
		},
		{
			// A target disc that contains the source vertex
			src:           "0:0",
			dest:          "0:2",
			target:        "0:0.1",
			avoid:         "",
			discOnLeft:    nil,
			radiusDegrees: s1.Angle(1.0) * s1.Degree,
			want:          true,
		},
		{
			// A target disc that contains the destination vertex.
			src:           "0:0",
			dest:          "0:2",
			target:        "0:2.1",
			avoid:         "",
			discOnLeft:    nil,
			radiusDegrees: s1.Angle(1.0) * s1.Degree,
			want:          true,
		},
		// Avoid one point.
		{
			// Three points on a straight line, attempting to avoid the middle point.
			src:           "0:0",
			dest:          "0:2",
			target:        "",
			avoid:         "0:1",
			discOnLeft:    []bool{true},
			radiusDegrees: s1.Angle(1e-10) * s1.Degree,
			want:          false,
		},
		{
			// Three points where the middle point can be successfully avoided.
			src:           "0:0",
			dest:          "0:2",
			target:        "",
			avoid:         "1:1",
			discOnLeft:    []bool{true},
			radiusDegrees: s1.Angle(0.9) * s1.Degree,
			want:          true,
		},
		{
			// Three points where the middle point is on the left, but where the client
			// requires the point to be on the right of the edge.
			src:           "0:0",
			dest:          "0:2",
			target:        "",
			avoid:         "1:1",
			discOnLeft:    []bool{false},
			radiusDegrees: s1.Angle(1e-10) * s1.Degree,
			want:          false,
		},
		{
			// Check cases where the point to be avoided is behind the source vertex.
			// In this situation "disc_on_left" should not affect the result.
			src:           "0:0",
			dest:          "0:2",
			target:        "",
			avoid:         "1:-1",
			discOnLeft:    []bool{false},
			radiusDegrees: s1.Angle(1.4) * s1.Degree,
			want:          true,
		},
		{
			src:           "0:0",
			dest:          "0:2",
			target:        "",
			avoid:         "1:-1",
			discOnLeft:    []bool{true},
			radiusDegrees: s1.Angle(1.4) * s1.Degree,
			want:          true,
		},
		{
			src:           "0:0",
			dest:          "0:2",
			target:        "",
			avoid:         "-1:-1",
			discOnLeft:    []bool{false},
			radiusDegrees: s1.Angle(1.4) * s1.Degree,
			want:          true,
		},
		{
			src:           "0:0",
			dest:          "0:2",
			target:        "",
			avoid:         "-1:-1",
			discOnLeft:    []bool{true},
			radiusDegrees: s1.Angle(1.4) * s1.Degree,
			want:          true,
		},

		// Avoid several points.
		// Tests that when several discs are avoided but none are targeted, the
		// range of acceptable edge directions is not represented a single interval.
		// (The output edge can proceed through any gap between the discs as long as
		// the "disc_on_left" criteria are satisfied.)
		//
		// This test involves 3 very small discs spaced 120 degrees apart around the
		// source vertex, where "disc_on_left" is true for all discs.  This means
		// that each disc blocks the 90 degrees of potential output directions just
		// to its left, leave 3 gaps measuring about 30 degrees each.
		{
			src:           "0:0",
			dest:          "0:2",
			target:        "",
			avoid:         "0.01:2, 1.732:-1.01, -1.732:-0.99",
			discOnLeft:    []bool{true, true, true},
			radiusDegrees: s1.Angle(0.00001) * s1.Degree,
			want:          true,
		},
		{
			src:           "0:0",
			dest:          "1.732:-1",
			target:        "",
			avoid:         "0.01:2, 1.732:-1.01, -1.732:-0.99",
			discOnLeft:    []bool{true, true, true},
			radiusDegrees: s1.Angle(0.00001) * s1.Degree,
			want:          true,
		},
		{
			src:           "0:0",
			dest:          "-1.732:-1",
			target:        "",
			avoid:         "0.01:2, 1.732:-1.01, -1.732:-0.99",
			discOnLeft:    []bool{true, true, true},
			radiusDegrees: s1.Angle(0.00001) * s1.Degree,
			want:          true,
		},
		// Also test that directions prohibited by "disc_on_left" are avoided.
		{
			src:           "0:0",
			dest:          "0:2",
			target:        "",
			avoid:         "0.01:2, 1.732:-1.01, -1.732:-0.99",
			discOnLeft:    []bool{false, false, false},
			radiusDegrees: s1.Angle(0.00001) * s1.Degree,
			want:          false,
		},
		{
			src:           "0:0",
			dest:          "1.732:-1",
			target:        "",
			avoid:         "0.01:2, 1.732:-1.01, -1.732:-0.99",
			discOnLeft:    []bool{false, false, false},
			radiusDegrees: s1.Angle(0.00001) * s1.Degree,
			want:          false,
		},
		{
			src:           "0:0",
			dest:          "-1.732:-1",
			target:        "",
			avoid:         "0.01:2, 1.732:-1.01, -1.732:-0.99",
			discOnLeft:    []bool{false, false, false},
			radiusDegrees: s1.Angle(0.00001) * s1.Degree,
			want:          false,
		},
		// TargetAndAvoid
		// Target several points that are separated from the proposed edge by about
		// 0.7 degrees, and avoid several points that are separated from the
		// proposed edge by about 1.4 degrees.
		{
			src:           "0:0",
			dest:          "10:10",
			target:        "2:3, 4:3, 7:8",
			avoid:         "4:2, 7:5, 7:9",
			discOnLeft:    []bool{true, true, false},
			radiusDegrees: s1.Angle(1.0) * s1.Degree,
			want:          true,
		},
		// The same example, but one point to be targeted is 1.4 degrees away.
		{
			src:           "0:0",
			dest:          "10:10",
			target:        "2:3, 4:6, 7:8",
			avoid:         "4:2, 7:5, 7:9",
			discOnLeft:    []bool{true, true, false},
			radiusDegrees: s1.Angle(1.0) * s1.Degree,
			want:          false,
		},
		// The same example, but one point to be avoided is 0.7 degrees away.
		{
			src:           "0:0",
			dest:          "10:10",
			target:        "2:3, 4:3, 7:8",
			avoid:         "4:2, 6:5, 7:9",
			discOnLeft:    []bool{true, true, false},
			radiusDegrees: s1.Angle(1.0) * s1.Degree,
			want:          false,
		},
	}

	for i, test := range tests {
		rad := s1.ChordAngleFromAngle(test.radiusDegrees)
		s := NewPolylineSimplifier(parsePoint(test.src))

		for _, p := range parsePoints(test.target) {
			s.TargetDisc(p, rad)
		}

		for i, p := range parsePoints(test.avoid) {
			s.CanAvoidDisc(p, rad, test.discOnLeft[i])
		}

		if got := s.CanExtend(parsePoint(test.dest)); got != test.want {
			t.Errorf("%d: s.Extend(%+v) = %v, want = %t", i, test.dest, got, test.want)
		}

	}
}

// TODO(rsned): Differences from C++
// TEST(S2PolylineSimplifier, Precision)
