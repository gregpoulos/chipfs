package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gregpoulos/chipfs/internal/vfs"
	"github.com/hanwen/go-fuse/v2/fs"
	gofuse "github.com/hanwen/go-fuse/v2/fuse"
)

type config struct {
	source     string
	mountpoint string
	allowOther bool
}

func parseArgs(args []string) (config, error) {
	fset := flag.NewFlagSet("chipfs", flag.ContinueOnError)
	source := fset.String("source", "", "path to directory containing chiptune files (required)")
	mountpoint := fset.String("mountpoint", "", "path to FUSE mount point (required)")
	allowOther := fset.Bool("allow_other", false, "allow other users (e.g. Docker containers) to access the mount")
	if err := fset.Parse(args); err != nil {
		return config{}, err
	}
	if *source == "" || *mountpoint == "" {
		fset.Usage()
		return config{}, fmt.Errorf("both -source and -mountpoint are required")
	}
	return config{source: *source, mountpoint: *mountpoint, allowOther: *allowOther}, nil
}

func main() {
	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "chipfs:", err)
		os.Exit(1)
	}
	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "chipfs: %v\n", err)
		os.Exit(1)
	}
}

func run(cfg config) error {
	root, err := vfs.NewRoot(cfg.source)
	if err != nil {
		return err
	}

	mountOpts := &fs.Options{
		MountOptions: gofuse.MountOptions{
			AllowOther: cfg.allowOther,
		},
	}

	server, err := fs.Mount(cfg.mountpoint, root, mountOpts)
	if err != nil {
		return fmt.Errorf("mounting %s: %w", cfg.mountpoint, err)
	}
	fmt.Printf("chipfs: mounted %s → %s\n", cfg.source, cfg.mountpoint)
	fmt.Printf("chipfs: press Ctrl-C to unmount\n")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Println("\nchipfs: unmounting…")
		server.Unmount()
	}()

	server.Wait()
	return nil
}
