package main

import (
	"flag"
	"fmt"
	"os"

	"hearth/internal/appupdate"
)

func main() {
	planPath := flag.String("plan", "", "path to the validated Hearth update plan")
	flag.Parse()
	if *planPath == "" {
		fmt.Fprintln(os.Stderr, "-plan is required")
		os.Exit(2)
	}
	plan, err := appupdate.ReadPlan(*planPath)
	if err == nil {
		err = appupdate.RunUpdater(plan)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
