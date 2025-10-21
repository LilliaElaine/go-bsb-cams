package main

import (
	"bytes"
	"flag"
	"fmt"
	"image/jpeg"
	"log"
	"net/http"
	"strconv"
	"syscall"

	"github.com/google/gousb"
	"github.com/hybridgroup/mjpeg"
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

var verbosePtr = flag.Bool("verbose", false, "Whether or not to show libusb errors")
var port = flag.Int("port", 8080, "What Port To Output Frames To (Default is 8080)")

func main() {
	flag.Parse()
	stream := mjpeg.NewLiveStream()
	device := getdevice()
	// Pass your jpegBuffer frames using stream.UpdateJPEG(<your-buffer>)
	go imagestreamer(stream, device)
	mux := http.NewServeMux()
	mux.Handle("/stream", stream)
	log.Print("Server Is Running And Can Be Accessed At: http://localhost:" + strconv.Itoa(*port) + "/stream")
	log.Print("Make Sure You Have No Ending / When Inputting The Url Into Baballonia !!!")
	log.Fatal(http.ListenAndServe(":"+strconv.Itoa(*port), mux))

}

func imagestreamer(stream *mjpeg.Stream, device string) {
frame:
	fd, err := syscall.Open(device, syscall.O_RDWR, 0)
	var deviceFd = fd
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
				log.Print("Yes")
				panic(err)
			}
			for {
				fr, err := resp.ReadFrame()
				if err != nil {
					if *verbosePtr {
						log.Print(err)
						log.Print("Reclaiming Frame Reader and continuing to get frames... ")
					}
					syscall.Close(deviceFd)
					goto frame
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
