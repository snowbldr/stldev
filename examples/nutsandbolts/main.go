package main

import (
	"github.com/snowbldr/fluent-sdfx/obj"
	"github.com/snowbldr/fluent-sdfx/units"
)

// Tolerance: Measured in mm. Typically 0.0 to 0.4. Larger is looser.
// Smaller is tighter.
const mmTolerance = 0.3
const inchTolerance = mmTolerance / units.MillimetresPerInch

// Quality: The long axis of the model is rendered with n cells.
const quality = 200

func inch() {
	// bolt
	obj.Bolt(obj.BoltParms{
		Thread:      "unc_5/8",
		Style:       "knurl",
		Tolerance:   inchTolerance,
		TotalLength: 2.0,
		ShankLength: 0.5,
	}).ScaleUniform(units.MillimetresPerInch).ToSTL("inch_bolt.stl", quality)

	// nut
	obj.Nut(obj.NutParms{
		Thread:    "unc_5/8",
		Style:     "knurl",
		Tolerance: inchTolerance,
	}).ScaleUniform(units.MillimetresPerInch).ToSTL("inch_nut.stl", quality)
}

func metric() {
	// bolt
	obj.Bolt(obj.BoltParms{
		Thread:      "M16x2",
		Style:       "hex",
		Tolerance:   mmTolerance,
		TotalLength: 1250.0,
		ShankLength: 10.0,
	}).ToSTL("metric_bolt.stl", quality)

	// nut
	obj.Nut(obj.NutParms{
		Thread:    "M16x2",
		Style:     "hex",
		Tolerance: mmTolerance,
	}).ToSTL("metric_nut.stl", quality)
}

func main() {
	inch()
	metric()
}
