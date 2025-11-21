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

	"github.com/hybridgroup/mjpeg"
	gouvc "github.com/visago/go-uvc"
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

	// Use frame buffer to prevent tearing
	frameBuf := &FrameBuffer{}

	// Start frame reader goroutine
	go imagestreamer(frameBuf)

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

func imagestreamer(frameBuf *FrameBuffer) {
	uvc := &gouvc.UVC{}
	if err := uvc.Init(); err != nil {
		log.Fatal("init: ", err)
	}
	defer uvc.Exit()

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

	frame:
	device, err := uvc.FindDevice(0x35bd, 0x0202, "")
	if err != nil {
		log.Print("Could Not Find Device, Please Make Sure It Is On And Plugged In !")
		log.Fatal("Error finding device: ", err)
	}
	defer device.Unref()
	// desc, _ := device.Descriptor()
	// log.Println("Found BSB2e cameras:\n", desc)
	log.Println("Found BSB2e cameras")

	if err := device.Open(); err != nil {
		log.Println("Error opening cameras: ",err)
		log.Print("It looks like the cameras cannot be accessed, udev file being created in this directory")
		log.Printf("Creating UDEV Rule At %v", udevfilename)
		err := os.WriteFile(udevfilename, []byte(udevrule), 0644)
		if err != nil {
			log.Fatalf("Could not Create File: %v", err)
		}
		log.Print("File Created ! Please copy to your udev directory, chown to root, and reboot for it to take effect")
		os.Exit(0)
	}
	defer device.Close()


	format := gouvc.FRAME_FORMAT_MJPEG
	width := 800
	height := 400
	fps := 90

	for _, si := range device.StreamInterfaces() {
		for _, formatDesc := range si.FormatDescriptors() {
			for _, frameDesc := range formatDesc.FrameDescriptors() {
				switch formatDesc.Subtype {
				case gouvc.VS_FORMAT_MJPEG:
					format = gouvc.FRAME_FORMAT_MJPEG
				case gouvc.VS_FORMAT_FRAME_BASED:
					// format = gouvc.FRAME_FORMAT_H264
				default:
					format = gouvc.FRAME_FORMAT_YUYV
				}
				width = int(frameDesc.Width)
				height = int(frameDesc.Height)
				fps = int(10000000 / frameDesc.DefaultFrameInterval)
				break
			}
		}
	}

	stream, err := device.GetStream(format, width, height, fps)
	if err != nil {
		log.Fatal("Error getting stream: ", err)
	}
	if err := stream.Open(); err != nil {
		log.Fatal("Error opening stream: ", err)
	}
	defer stream.Close()

	cf, err := stream.Start()
	if err != nil {
		log.Fatal("Error starting stream: ", err)
	}
	defer stream.Stop()

	for {
		select {
		case frame := <-cf:
			jpegbuf := new(bytes.Buffer)
			if _, err := jpegbuf.ReadFrom(frame); err != nil {
				if *verbosePtr {
					log.Printf("Failed to read frame: %v", err)
					log.Print("Retying device and continuing to get frames... ")
				}
				goto frame
			}
			frameBuf.Update(jpegbuf.Bytes())
		}
	}
}
