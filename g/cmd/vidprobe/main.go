package main

import (
	"fmt"
	"os"

	"pygorvid/vid"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: vidprobe FILE...")
		os.Exit(2)
	}
	status := 0
	for _, p := range os.Args[1:] {
		v := vid.OpenFile(p)
		if v.IsOpen() {
			info := v.GetBasicInfo()
			if err := v.ErrorInfo(); err != "" {
				fmt.Fprintf(os.Stderr, "%s: %s: %s\n", p, v.Format, err)
				status = 1
			} else {
				fmt.Printf("%s: %s hasvideo=%t hasaudio=%t\n", p, v.Format, info.HasVideo, info.HasAudio)
			}
		} else {
			fmt.Fprintf(os.Stderr, "%s: %s\n", p, v.ErrorInfo())
			status = 1
		}
		v.Close()
	}
	os.Exit(status)
}
