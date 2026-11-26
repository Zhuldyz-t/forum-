package handlers

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"io"

	_ "image/gif"
	_ "image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// processImage decodes any supported image, applies the JPEG EXIF
// orientation and scales it down so its longest side fits maxSide, keeping
// proportions (avatars and covers are cropped by CSS). The result is always
// a JPEG, which also strips any metadata (EXIF, GPS) from the upload.
func processImage(data []byte, maxSide int) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	src = applyOrientation(src, jpegOrientation(data))

	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > maxSide || h > maxSide {
		if w >= h {
			h = h * maxSide / w
			w = maxSide
		} else {
			w = w * maxSide / h
			h = maxSide
		}
	}
	w, h = max(w, 1), max(h, 1)

	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	// White background so transparent PNG/GIF areas don't turn black in JPEG.
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// jpegOrientation returns the EXIF orientation tag (1–8) of a JPEG, or 1 if
// there is none. Phone cameras store rotation here instead of in the pixels.
func jpegOrientation(data []byte) int {
	r := bytes.NewReader(data)
	var marker [2]byte
	if _, err := io.ReadFull(r, marker[:]); err != nil || marker != [2]byte{0xFF, 0xD8} {
		return 1
	}
	for {
		if _, err := io.ReadFull(r, marker[:]); err != nil || marker[0] != 0xFF {
			return 1
		}
		var segLen uint16
		if err := binary.Read(r, binary.BigEndian, &segLen); err != nil || segLen < 2 {
			return 1
		}
		seg := make([]byte, segLen-2)
		if _, err := io.ReadFull(r, seg); err != nil {
			return 1
		}
		if marker[1] == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return exifOrientation(seg[6:])
		}
		if marker[1] == 0xDA { // start of scan: no more metadata
			return 1
		}
	}
}

func exifOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	off := int(bo.Uint32(tiff[4:8]))
	if off+2 > len(tiff) {
		return 1
	}
	n := int(bo.Uint16(tiff[off:]))
	for i := 0; i < n; i++ {
		e := off + 2 + i*12
		if e+12 > len(tiff) {
			return 1
		}
		if bo.Uint16(tiff[e:]) == 0x0112 {
			v := int(bo.Uint16(tiff[e+8:]))
			if v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}

// applyOrientation transforms the image so it displays upright.
func applyOrientation(src image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
