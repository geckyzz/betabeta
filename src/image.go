// This file is part of betabeta.
// Copyright (C) 2026  geckyzz <gecko.geckyzz@proton.me>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"fmt"
	"strings"
)

// pullLatestGluetunImage ensures the qmcgaw/gluetun image is up to date.
func pullLatestGluetunImage() error {
	prni("Pulling latest qmcgaw/gluetun image...")
	_, stderr, code := podmanCmd("pull", "qmcgaw/gluetun")
	if code != 0 {
		return fmt.Errorf("podman pull failed (code %d): %s", code, stderr)
	}
	prni("✓ qmcgaw/gluetun image is up to date")
	return nil
}

// purgeUnusedGluetunImages removes qmcgaw/gluetun images that are not
// referenced by any existing container (running or stopped).
func purgeUnusedGluetunImages() error {
	// List all images with their repository names.
	out, stderr, code := podmanCmd("images", "--format", "{{.ID}}\t{{.Repository}}")
	if code != 0 {
		return fmt.Errorf("podman images failed (code %d): %s", code, stderr)
	}

	// Collect image IDs currently in use by any container.
	used, _, _ := podmanCmd("ps", "-a", "--format", "{{.Image}}")
	usedSet := make(map[string]bool)
	for _, line := range strings.Split(used, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		usedSet[line] = true
	}

	purged := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		imageID := parts[0]
		imageRef := parts[1]

		if imageRef != "qmcgaw/gluetun" {
			continue
		}

		// Skip if this image (by ID or repository name) is in use by a container.
		if usedSet[imageID] || usedSet[imageRef] || usedSet[strings.TrimSpace(imageRef)] {
			continue
		}

		_, stderr, code := podmanCmd("rmi", imageID)
		if code != 0 {
			prnv("Skipping removal of %s (%s): %s", imageRef, imageID, strings.TrimSpace(stderr))
			continue
		}
		prni("✓ Purged unused gluetun image %s", imageRef)
		purged++
	}

	if purged == 0 {
		prni("No unused gluetun images to purge")
	} else {
		prni("✓ Purged %d unused gluetun image(s)", purged)
	}
	return nil
}

// ensureGluetunImage pulls the latest image and purges unused ones.
func ensureGluetunImage() error {
	if err := pullLatestGluetunImage(); err != nil {
		prnv("Warning: failed to pull latest gluetun image: %v", err)
		prni("Continuing with locally available image")
	}
	if err := purgeUnusedGluetunImages(); err != nil {
		prnv("Warning: failed to purge unused gluetun images: %v", err)
	}
	return nil
}
