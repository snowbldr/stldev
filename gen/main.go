// Generates the placeholder STL that stldev writes over the user's target
// STLs during a rebuild. It's a 3D grid of dots spanning a large volume — no
// matter how the user has zoomed or panned their f3d view, several dots
// should be in frame, making the "build in progress" state visible.
//
// The output is committed at ../loading.stl and embedded into stldev via
// go:embed. Run from this directory:
//
//	go run .
package main

import (
	"flag"
	"fmt"

	"github.com/snowbldr/fluent-sdfx/solid"
	v3 "github.com/snowbldr/fluent-sdfx/vec/v3"
)

func main() {
	out := flag.String("o", "../loading.stl", "output STL path")
	radius := flag.Float64("radius", 12, "dot radius in mm")
	step := flag.Float64("step", 80, "grid spacing in mm")
	count := flag.Int("count", 5, "dots per axis (odd, so a dot lands on the origin)")
	res := flag.Int("res", 400, "mesh resolution (high — decimation keeps file small)")
	keep := flag.Float64("keep", 0.05, "fraction of triangles to keep after simplification")
	flag.Parse()

	dots := solid.Sphere(*radius).Array(*count, *count, *count, v3.XYZ(*step, *step, *step))
	dots.ToSTL(*out, *res, 1-*keep)
	fmt.Printf("wrote %s\n", *out)
}
