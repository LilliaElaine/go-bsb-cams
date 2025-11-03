package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/google/gousb"
	"github.com/hybridgroup/mjpeg"
	"github.com/kevmo314/go-uvc"
	"github.com/kevmo314/go-uvc/pkg/descriptors"
)

const udevrule = `# Bigscreen Bigeye
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="35bd", ATTRS{idProduct}=="0202", MODE="0660", GROUP="users", TAG+="uaccess"
SUBSYSTEM=="usb", ATTRS{idVendor}=="35bd", ATTRS{idProduct}=="0202", MODE="0660", GROUP="users", TAG+="uaccess"
`
const udevfilename = "99-bsb-cams.rules"

type FrameBuffer struct {
	mu    sync.Mutex
	frame []byte
}

func (fb *FrameBuffer) Update(data []byte) {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	// Create a copy to store internally
	if fb.frame == nil || len(fb.frame) != len(data) {
		fb.frame = make([]byte, len(data))
	}
	copy(fb.frame, data)
}

func (fb *FrameBuffer) Get() []byte {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	if fb.frame == nil {
		return nil
	}
	// Return a copy to prevent tearing
	frameCopy := make([]byte, len(fb.frame))
	copy(frameCopy, fb.frame)
	return frameCopy
}

func getdevice() (device string) {
	ctx := gousb.NewContext()
	defer ctx.Close()
	dev, err := ctx.OpenDeviceWithVIDPID(0x35bd, 0x0202)
	user, _ := user.Current()
	if user.Username == "root" && !*sudo {
		log.Print("Running As Root Isnt Reccomended For Safety, Creating A UDEV Rule To Allow Rootless Access ")
		if _, err := os.Stat(udevfilename); err == nil {
			log.Print("File Already Exists")
		} else {
			err := os.WriteFile(udevfilename, []byte(udevrule), 0644)
			if err != nil {
				log.Fatalf("Could not Create File: %v", err)
			}
		}
		var response string
		log.Print("Would You Like The UDEV Rule Automatically Moved And Put In Place? (Y/N)")
		fmt.Scan(&response)
		switch strings.ToLower(response) {
		case "yes", "ye", "y":
			log.Printf("Would You Like The Rule Moved To The Userspace (/usr/lib/udev/rules.d/%v) Or The Linux OS Space (/etc/udev/rules.d/%v) (Good For Atomic Operating Systems) ", udevfilename)
			log.Print("0/Userspace    1/OS Space")
			fmt.Scan(&response)
			switch strings.ToLower(response) {
			case "0", "userspace":
				log.Printf("Putting File In /usr/lib/udev/rules.d/%v !!", udevfilename)
				err := os.Rename(udevfilename, "/usr/lib/udev/rules.d/"+udevfilename)
				if err != nil {
					log.Fatalf("Could Not Move File: %v", err)
				}
			case "1", "os space":
				log.Printf("Putting File In /etc/udev/rules.d/%v !!", udevfilename)
				err := os.Rename(udevfilename, "/etc/udev/rules.d/"+udevfilename)
				if err != nil {
					log.Fatalf("Could Not Move File: %v", err)
				}
			default:
				log.Fatal("Invalid Response")
			}
			log.Print("Please Reboot Your PC For Changes To Take Effect!!")
			os.Exit(0)
		case "n", "no":
			log.Printf("Please move the file (%v) into your udev rule directory and reboot for it to take effect, or if you REALLLLY want to run this program as sudo append --sudo to your run command", udevfilename)
			os.Exit(0)
		default:
			log.Fatal("Invalid Answer")
		}
	}
	if err != nil {
		if err == gousb.ErrorAccess {
			log.Print("It looks like the cameras cannot be accessed, udev file being created in this directory")
			log.Printf("Creating UDEV Rule At %v", udevfilename)
			err := os.WriteFile(udevfilename, []byte(udevrule), 0644)
			if err != nil {
				log.Fatalf("Could not Create File: %v", err)
			}
			log.Print("File Created ! Please copy to your udev directory, chown to root, and reboot for it to take effect")
			os.Exit(0)

		} else {
			log.Fatal(err)
		}
	}
	if dev == nil {
		log.Fatal("Could Not Find Device, Please Make Sure It Is On And Plugged In !")
	}
	defer dev.Close()
	return fmt.Sprintf("/dev/bus/usb/%03v/%03v", dev.Desc.Bus, dev.Desc.Address)
}

var gitVersion string
var verbosePtr = flag.Bool("verbose", false, "Whether or not to show libusb errors")
var port = flag.Int("port", 8080, "What Port To Output Frames To")
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

	// Use frame buffer to prevent tearing
	frameBuf := &FrameBuffer{}

	// Start frame reader goroutine
	go imagestreamer(frameBuf, device)

	// Start frame delivery goroutine that continuously pushes latest frame to stream
	go func() {
		for {
			frame := frameBuf.Get()
			if frame != nil {
				stream.UpdateJPEG(frame)
			}
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/stream", stream)
	log.Print("Server Is Running And Can Be Accessed At: http://localhost:" + strconv.Itoa(*port) + "/stream")
	log.Print("Make Sure You Have No Ending / When Inputting The Url Into Baballonia !!!")
	log.Print("If You Are Here And Cannot See The Cams, Please Close This Program, Unplug And Replug Your BSB, And Try Again :)")
	log.Fatal(http.ListenAndServe(":"+strconv.Itoa(*port), mux))
}

func imagestreamer(frameBuf *FrameBuffer, device string) {
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

				// Read frame data into buffer
				jpegbuf := new(bytes.Buffer)
				if _, err := jpegbuf.ReadFrom(fr); err != nil {
					if *verbosePtr {
						log.Printf("failed to read frame: %v", err)
					}
					continue
				}

				// Atomically update the current frame - only complete frames are visible
				// This prevents tearing by ensuring frames are never partially written
				frameBuf.Update(jpegbuf.Bytes())
			}
		}
	}
}
