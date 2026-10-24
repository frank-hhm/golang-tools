//go:build windows

package screenshot

import (
	"image"
)

func SelectRegion() (rect image.Rectangle, ok bool, err error) {
	return selectRegionWindows()
}
