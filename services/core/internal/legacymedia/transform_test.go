package legacymedia

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"
)

func TestValidateSourceImageRejectsExtensionMismatch(t *testing.T) {
	raw := encodeJPEG(t, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	if _, err := ValidateSourceImage(raw, ".png"); err == nil {
		t.Fatal("ValidateSourceImage accepted JPEG bytes with a PNG extension")
	}
}

func TestBuildStoredImagePreservesExactSourceBytes(t *testing.T) {
	raw := encodeJPEG(t, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	metadata, err := ValidateSourceImage(raw, ".jpg")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := buildStoredImage(raw, metadata)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored.Bytes, raw) || stored.Metadata != metadata || stored.Descriptor.ByteLength != int64(len(raw)) {
		t.Fatalf("stored image = %+v", stored)
	}
}

func encodeJPEG(t *testing.T, value image.Image) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, value, nil); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
