PYTHON ?= python3

.PHONY: release release-python release-go release-rust release-cpp test-release

release:
	PYTHON="$(PYTHON)" bash scripts/build_release.sh all

release-python:
	PYTHON="$(PYTHON)" bash scripts/build_release.sh python

release-go:
	bash scripts/build_release.sh go

release-rust:
	bash scripts/build_release.sh rust

release-cpp:
	bash scripts/build_release.sh cpp

test-release:
	PYTHON="$(PYTHON)" bash scripts/test_release.sh