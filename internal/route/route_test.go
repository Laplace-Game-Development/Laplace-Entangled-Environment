package route

import (
	"flag"
	"fmt"
	"log"
	"os"
	"testing"
)

var (
	cwd_arg = flag.String("cwd", "", "set cwd")
)

func TestMain(m *testing.M) {
	flag.Parse()
	if *cwd_arg != "" {
		if err := os.Chdir(*cwd_arg); err != nil {
			fmt.Println("Chdir error:", err)
		}
	} else {
		// Skip These Tests
		log.Fatalf("CWD Variable is Empty!")
		return
	}

	os.Exit(m.Run())
}
