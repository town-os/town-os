package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gitea.com/town-os/town-os/src/upnp"
)

type portFlag struct {
	pairs []portPair
}

type portPair struct {
	external uint16
	internal uint16
}

func (f *portFlag) String() string {
	parts := make([]string, len(f.pairs))
	for i, p := range f.pairs {
		parts[i] = fmt.Sprintf("%d:%d", p.external, p.internal)
	}
	return strings.Join(parts, ",")
}

func (f *portFlag) Set(value string) error {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("expected format ext:int, got %q", value)
	}

	ext, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		return fmt.Errorf("invalid external port %q: %w", parts[0], err)
	}

	int_, err := strconv.ParseUint(parts[1], 10, 16)
	if err != nil {
		return fmt.Errorf("invalid internal port %q: %w", parts[1], err)
	}

	f.pairs = append(f.pairs, portPair{external: uint16(ext), internal: uint16(int_)})
	return nil
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: town-os-upnp <add|remove> [flags]")
	}

	subcmd := os.Args[1]

	switch subcmd {
	case "add":
		return runAdd(os.Args[2:])
	case "remove":
		return runRemove(os.Args[2:])
	default:
		return fmt.Errorf("unknown subcommand %q; use add or remove", subcmd)
	}
}

func runAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	var ports portFlag
	fs.Var(&ports, "port", "port mapping as ext:int (repeatable)")
	ttl := fs.Uint("ttl", 600, "TTL in seconds for port mapping")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if len(ports.pairs) == 0 {
		return fmt.Errorf("at least one --port is required")
	}

	client, err := upnp.NewIGDClient()
	if err != nil {
		return fmt.Errorf("discover IGD: %w", err)
	}

	for _, p := range ports.pairs {
		desc := fmt.Sprintf("town-os port %d", p.external)
		if err := client.AddPortMapping("TCP", p.external, p.internal, desc, uint32(*ttl)); err != nil {
			return fmt.Errorf("add mapping %d:%d: %w", p.external, p.internal, err)
		}
		fmt.Fprintf(os.Stderr, "added TCP %d -> %d (ttl %ds)\n", p.external, p.internal, *ttl)
	}

	return nil
}

func runRemove(args []string) error {
	fs := flag.NewFlagSet("remove", flag.ExitOnError)
	var ports portFlag
	fs.Var(&ports, "port", "port mapping as ext:int (repeatable)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if len(ports.pairs) == 0 {
		return fmt.Errorf("at least one --port is required")
	}

	client, err := upnp.NewIGDClient()
	if err != nil {
		return fmt.Errorf("discover IGD: %w", err)
	}

	for _, p := range ports.pairs {
		if err := client.RemovePortMapping("TCP", p.external); err != nil {
			return fmt.Errorf("remove mapping %d: %w", p.external, err)
		}
		fmt.Fprintf(os.Stderr, "removed TCP %d\n", p.external)
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "town-os-upnp: %v\n", err)
		os.Exit(1)
	}
}
