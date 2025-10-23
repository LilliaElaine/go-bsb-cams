package main

import (
	"bytes"
	"flag"
	"fmt"
	"image/jpeg"
	"log"
	"net/http"
	"os"
	"os/user"
	"strconv"
	"strings"
	"syscall"

	"github.com/google/gousb"
	"github.com/hybridgroup/mjpeg"
	"github.com/kevmo314/go-uvc"
	"github.com/kevmo314/go-uvc/pkg/descriptors"
)

const udevrule = `# Bigscreen Bigeye
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="35bd", ATTRS{idProduct}=="0202", MODE="0660", TAG+="uaccess"
SUBSYSTEM=="usb", ATTRS{idVendor}=="35bd", ATTRS{idProduct}=="0202", MODE="0660", TAG+="uaccess"
`
const path = "99-bsb-cams.rules"

func getdevice() (device string) {
	ctx := gousb.NewContext()
	defer ctx.Close()
	dev, err := ctx.OpenDeviceWithVIDPID(0x35bd, 0x0202)
	user, _ := user.Current()
	if user.Username == "root" && !*sudo {
		log.Print("Running As Root Isnt Reccomended For Safety, Creating A UDEV Rule To Allow Rootless Access ")
		if _, err := os.Stat(path); err == nil {
			log.Print("File Already Exists")
		} else {
			err := os.WriteFile(path, []byte(udevrule), 0644)
			if err != nil {
				log.Fatalf("Could not Create File: %v", err)
			}
		}
		var response string
		log.Printf("Would You Like The UDEV Rule Automatically Moved To Your UDEV DIR? It will be at /usr/lib/udev/rules.d/%v (Y/N)", path)
		fmt.Scan(&response)
		switch strings.ToLower(response) {
		case "yes", "ye", "y":
			log.Print("Moving File !!")
			if _, err := os.Stat(path); err == nil {
				log.Print("File Already Exists, Do You Want To Replace It? (Y/N)")
				fmt.Scan(&response)
				switch strings.ToLower(response) {
				case "yes", "ye", "y":
					log.Print("Replacing File !")
				case "n", "no":
					os.Exit(0)
				default:
					log.Fatal("Invalid Response")
				}
			}
			err := os.Rename(path, "/usr/lib/udev/rules.d/"+path)
			if err != nil {
				log.Fatalf("Could Not Move File: %v", err)
			}
			log.Print("Please Reboot Your PC For Changes To Take Effect!!")
			os.Exit(0)
		case "n", "no":
			log.Printf("Please move the file (%v) into your udev rule directory and reboot for it to take effect, or if you REALLLLY want to run this program as sudo append --sudo to your run command", path)
			os.Exit(0)
		default:
			log.Fatal("Invalid Answer")
		}
	}
	if err != nil {
		if err == gousb.ErrorAccess {
			log.Print("It looks like the cameras cannot be accessed, udev file being created in this directory")
			log.Printf("Creating UDEV Rule At %v", path)
			if _, err := os.Stat(path); err == nil {
				log.Print("File Already Exists")
			} else {
				err := os.WriteFile(path, []byte(udevrule), 0644)
				if err != nil {
					log.Fatalf("Could not Create File: %v", err)
				}
			}
			log.Print("File Created ! Please copy to your udev directory, chown to root, and reboot for it to take effect")
			os.Exit(0)

		}
	}
	defer dev.Close()
	return fmt.Sprintf("/dev/bus/usb/%03v/%03v", dev.Desc.Bus, dev.Desc.Address)
}

var gitVersion string
var verbosePtr = flag.Bool("verbose", false, "Whether or not to show libusb errors")
var port = flag.Int("port", 8080, "What Port To Output Frames To (Default is 8080)")
var version = flag.Bool("version", false, "Flag To Show Current Version")
var sudo = flag.Bool("sudo", false, "Force Program To Run As Sudo")

func main() {
	flag.Parse()
	if *version {
		log.Print("go-bsb-cams " + gitVersion)
		os.Exit(0)
	}
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
