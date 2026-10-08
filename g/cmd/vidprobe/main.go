package vidprobe
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
		f, err := vid.ProbeFile(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", p, err)
			status = 1
			continue
		}
		fmt.Printf("%s: %s\n", p, f)
	}
	os.Exit(status)
}
