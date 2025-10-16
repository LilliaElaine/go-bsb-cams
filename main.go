package main

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"log"
	"net/http"
	"syscall"

	// "github.com/garymcbay/mjpeg"
	"github.com/google/gousb"
	"github.com/kevmo314/go-uvc"
	"github.com/kevmo314/go-uvc/pkg/descriptors"
)

func getdevice() (device string) {
	ctx := gousb.NewContext()
	defer ctx.Close()
	dev, err := ctx.OpenDeviceWithVIDPID(0x35bd, 0x0202)
	if err != nil {
		log.Fatalf("Could not open a device: %v", err)
	}
	defer dev.Close()
	return fmt.Sprintf("/dev/bus/usb/%03v/%03v", dev.Desc.Bus, dev.Desc.Address)
}

func main() {
	stream := NewStream()
	device := getdevice()
	// Pass your jpegBuffer frames using stream.UpdateJPEG(<your-buffer>)
	go imagestreamer(stream, device)
	mux := http.NewServeMux()
	mux.Handle("/stream", stream)
	log.Fatal(http.ListenAndServe(":8080", mux))

}

func imagestreamer(stream *Stream, device string) {
	fd, err := syscall.Open(device, syscall.O_RDWR, 0)
	if err != nil {
		panic(err)
	}
	ctx, err := uvc.NewUVCDevice(uintptr(fd))
	if err != nil {
		panic(err)
	}

	info, err := ctx.DeviceInfo()
	if err != nil {
		panic(err)
	}
	for _, iface := range info.StreamingInterfaces {

		for i, desc := range iface.Descriptors {
			fd, ok := desc.(*descriptors.MJPEGFormatDescriptor)
			if !ok {
				continue
			}
			frd := iface.Descriptors[i+1].(*descriptors.MJPEGFrameDescriptor)

			resp, err := iface.ClaimFrameReader(fd.Index(), frd.Index())
			if err != nil {
				panic(err)
			}
			for {
				fr, err := resp.ReadFrame()
				if err != nil {
					panic(err)
				}

				img, err := jpeg.Decode(fr)
				if err != nil {
					continue
				}
				jpegbuf := new(bytes.Buffer)

				if err = jpeg.Encode(jpegbuf, img, nil); err != nil {
					log.Printf("failed to encode: %v", err)
				}
				// boundry := ("--frame-boundary\r\nContent-Type: image/jpeg\r\nContent-Length: " + strconv.Itoa(len(jpegbuf.Bytes())) + "\r\n\r\n")
				// stream.UpdateJPEG(append([]byte(boundry), jpegbuf.Bytes()...))
				stream.UpdateJPEG(jpegbuf.Bytes())
			}
		}
	}
}
