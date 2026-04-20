# Example Makefile showing how to wire stldev into a project.
#
#   make build           # render all STLs at full quality
#   make dev             # watch this dir, preview all 4 STLs
#   make dev bolts       # watch this dir, preview only the bolts
#   make dev nuts        # watch this dir, preview only the nuts
#   make dev inch        # watch this dir, preview only the inch pair
#   make dev metric      # watch this dir, preview only the metric pair

# Extra args passed through to every f3d window. `--watch` enables live reload
# (essential for the dev loop); the rest are taste.
DEV_F3D_ARGS ?= --watch --color=\#00FF00 --opacity=0.6

# Extract the subset name from goals: `make dev bolts` → SET=bolts
VALID_SETS = all bolts nuts inch metric
SET := $(or $(filter $(VALID_SETS),$(MAKECMDGOALS)),all)

ifeq ($(SET),bolts)
STLS = inch_bolt.stl metric_bolt.stl
else ifeq ($(SET),nuts)
STLS = inch_nut.stl metric_nut.stl
else ifeq ($(SET),inch)
STLS = inch_bolt.stl inch_nut.stl
else ifeq ($(SET),metric)
STLS = metric_bolt.stl metric_nut.stl
else
STLS = inch_bolt.stl inch_nut.stl metric_bolt.stl metric_nut.stl
endif

.PHONY: build dev $(VALID_SETS) check-stldev

build:
	go run .

dev: check-stldev
	stldev -cmd "go run ." -f3d-args "$(DEV_F3D_ARGS)" $(STLS)

# Swallow set names so make doesn't treat them as missing targets
$(VALID_SETS):
	@:

check-stldev:
	@command -v stldev >/dev/null 2>&1 || { echo "stldev not found. Install with: go install github.com/snowbldr/stldev@latest"; exit 1; }
