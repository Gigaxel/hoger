package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"
)

const defaultHostsPath = "/etc/hosts"

type entry struct {
	IP      string
	Hosts   []string
	Comment string
	LineNo  int
}

type hostsLine struct {
	raw     string
	ip      string
	hosts   []string
	comment string
	active  bool
}

type app struct {
	out io.Writer
	err io.Writer
}

func main() {
	a := app{out: os.Stdout, err: os.Stderr}
	if err := a.run(os.Args[1:]); err != nil {
		fmt.Fprintln(a.err, "hoger:", err)
		os.Exit(1)
	}
}

func (a app) run(args []string) error {
	fs := flag.NewFlagSet("hoger", flag.ContinueOnError)
	fs.SetOutput(a.err)
	hostsPath := fs.String("hosts", hostsPathFromEnv(), "hosts file path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rest := fs.Args()
	if len(rest) == 0 {
		a.usage()
		return nil
	}

	switch rest[0] {
	case "list":
		return a.list(*hostsPath, rest[1:])
	case "lookup":
		return a.lookup(*hostsPath, rest[1:])
	case "add":
		return a.add(*hostsPath, rest[1:])
	case "set":
		return a.set(*hostsPath, rest[1:])
	case "remove":
		return a.remove(*hostsPath, rest[1:])
	case "help", "-h", "--help":
		a.usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", rest[0])
	}
}

func (a app) usage() {
	fmt.Fprintln(a.out, `hoger manages local DNS entries in a hosts file.

Usage:
  hoger [-hosts PATH] list
  hoger [-hosts PATH] lookup HOST [HOST...]
  hoger [-hosts PATH] add IP HOST [HOST...]
  hoger [-hosts PATH] set HOST IP
  hoger [-hosts PATH] remove HOST [HOST...]

The default hosts file is /etc/hosts. Set HOGER_HOSTS or pass -hosts to target another file.
Writing to /etc/hosts usually requires sudo.`)
}

func hostsPathFromEnv() string {
	if path := os.Getenv("HOGER_HOSTS"); path != "" {
		return path
	}
	return defaultHostsPath
}

func (a app) list(path string, args []string) error {
	if len(args) != 0 {
		return errors.New("list takes no arguments")
	}
	entries, err := readEntries(path)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Fprintln(a.out, "No active hosts entries found.")
		return nil
	}

	width := 2
	for _, e := range entries {
		if len(e.IP) > width {
			width = len(e.IP)
		}
	}
	for _, e := range entries {
		fmt.Fprintf(a.out, "%-*s  %s", width, e.IP, strings.Join(e.Hosts, " "))
		if e.Comment != "" {
			fmt.Fprintf(a.out, "  # %s", e.Comment)
		}
		fmt.Fprintf(a.out, "  (line %d)\n", e.LineNo)
	}
	return nil
}

func (a app) lookup(path string, args []string) error {
	if len(args) == 0 {
		return errors.New("lookup requires at least one host")
	}
	entries, err := readEntries(path)
	if err != nil {
		return err
	}
	index := make(map[string][]entry)
	for _, e := range entries {
		for _, host := range e.Hosts {
			index[host] = append(index[host], e)
		}
	}

	for _, host := range args {
		matches := index[host]
		if len(matches) == 0 {
			fmt.Fprintf(a.out, "%s -> not found\n", host)
			continue
		}
		ips := make([]string, 0, len(matches))
		for _, match := range matches {
			ips = append(ips, fmt.Sprintf("%s (line %d)", match.IP, match.LineNo))
		}
		fmt.Fprintf(a.out, "%s -> %s\n", host, strings.Join(ips, ", "))
	}
	return nil
}

func (a app) add(path string, args []string) error {
	if len(args) < 2 {
		return errors.New("add requires IP and at least one host")
	}
	ip := args[0]
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("invalid IP address %q", ip)
	}
	hosts, err := normalizeHosts(args[1:])
	if err != nil {
		return err
	}

	lines, trailingNewline, err := readHostsLines(path)
	if err != nil {
		return err
	}
	existing := activeHostSet(lines)
	newHosts := make([]string, 0, len(hosts))
	for _, host := range hosts {
		if !existing[host] {
			newHosts = append(newHosts, host)
		}
	}
	if len(newHosts) == 0 {
		fmt.Fprintln(a.out, "No changes.")
		return nil
	}
	lines = addHostsToIP(lines, ip, newHosts)
	if err := writeHostsLines(path, lines, trailingNewline); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Added %s -> %s\n", strings.Join(newHosts, " "), ip)
	return nil
}

func (a app) set(path string, args []string) error {
	if len(args) != 2 {
		return errors.New("set requires HOST and IP")
	}
	host := strings.TrimSpace(args[0])
	ip := strings.TrimSpace(args[1])
	if err := validateHost(host); err != nil {
		return err
	}
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("invalid IP address %q", ip)
	}

	lines, trailingNewline, err := readHostsLines(path)
	if err != nil {
		return err
	}
	lines, _ = removeHostsFromLines(lines, map[string]bool{host: true})
	lines = addHostsToIP(lines, ip, []string{host})
	if err := writeHostsLines(path, lines, trailingNewline); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Set %s -> %s\n", host, ip)
	return nil
}

func (a app) remove(path string, args []string) error {
	if len(args) == 0 {
		return errors.New("remove requires at least one host")
	}
	hosts, err := normalizeHosts(args)
	if err != nil {
		return err
	}
	targets := make(map[string]bool, len(hosts))
	for _, host := range hosts {
		targets[host] = true
	}

	lines, trailingNewline, err := readHostsLines(path)
	if err != nil {
		return err
	}
	lines, removed := removeHostsFromLines(lines, targets)
	if len(removed) == 0 {
		fmt.Fprintln(a.out, "No changes.")
		return nil
	}
	if err := writeHostsLines(path, lines, trailingNewline); err != nil {
		return err
	}
	sort.Strings(removed)
	fmt.Fprintf(a.out, "Removed %s\n", strings.Join(removed, " "))
	return nil
}

func readEntries(path string) ([]entry, error) {
	lines, _, err := readHostsLines(path)
	if err != nil {
		return nil, err
	}
	entries := make([]entry, 0, len(lines))
	for i, line := range lines {
		if !line.active {
			continue
		}
		entries = append(entries, entry{
			IP:      line.ip,
			Hosts:   append([]string(nil), line.hosts...),
			Comment: line.comment,
			LineNo:  i + 1,
		})
	}
	return entries, nil
}

func readHostsLines(path string) ([]hostsLine, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	text := string(data)
	trailingNewline := strings.HasSuffix(text, "\n")
	if trailingNewline {
		text = strings.TrimSuffix(text, "\n")
	}
	if text == "" {
		return nil, trailingNewline, nil
	}
	rawLines := strings.Split(text, "\n")
	lines := make([]hostsLine, 0, len(rawLines))
	for _, raw := range rawLines {
		lines = append(lines, parseHostsLine(raw))
	}
	return lines, trailingNewline, nil
}

func parseHostsLine(raw string) hostsLine {
	line := hostsLine{raw: raw}
	hash := strings.Index(raw, "#")
	body := raw
	if hash >= 0 {
		body = raw[:hash]
		line.comment = strings.TrimSpace(raw[hash+1:])
	}
	fields := strings.Fields(body)
	if len(fields) < 2 || net.ParseIP(fields[0]) == nil {
		return line
	}
	line.ip = fields[0]
	line.hosts = append(line.hosts, fields[1:]...)
	line.active = true
	return line
}

func writeHostsLines(path string, lines []hostsLine, trailingNewline bool) error {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, renderHostsLine(line))
	}
	text := strings.Join(out, "\n")
	if trailingNewline || len(lines) > 0 {
		text += "\n"
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), info.Mode().Perm())
}

func renderHostsLine(line hostsLine) string {
	if !line.active {
		return line.raw
	}
	body := strings.Join(append([]string{line.ip}, line.hosts...), "\t")
	if line.comment != "" {
		return body + "\t# " + line.comment
	}
	return body
}

func activeHostSet(lines []hostsLine) map[string]bool {
	hosts := make(map[string]bool)
	for _, line := range lines {
		if !line.active {
			continue
		}
		for _, host := range line.hosts {
			hosts[host] = true
		}
	}
	return hosts
}

func addHostsToIP(lines []hostsLine, ip string, hosts []string) []hostsLine {
	for i := range lines {
		if lines[i].active && lines[i].ip == ip {
			lines[i].hosts = append(lines[i].hosts, hosts...)
			return lines
		}
	}
	lines = append(lines, hostsLine{
		ip:     ip,
		hosts:  append([]string(nil), hosts...),
		active: true,
	})
	return lines
}

func removeHostsFromLines(lines []hostsLine, targets map[string]bool) ([]hostsLine, []string) {
	removedSet := make(map[string]bool)
	next := make([]hostsLine, 0, len(lines))
	for _, line := range lines {
		if !line.active {
			next = append(next, line)
			continue
		}
		kept := line.hosts[:0]
		for _, host := range line.hosts {
			if targets[host] {
				removedSet[host] = true
				continue
			}
			kept = append(kept, host)
		}
		line.hosts = kept
		if len(line.hosts) == 0 {
			if line.comment != "" {
				next = append(next, hostsLine{raw: "# " + line.comment})
			}
			continue
		}
		next = append(next, line)
	}
	removed := make([]string, 0, len(removedSet))
	for host := range removedSet {
		removed = append(removed, host)
	}
	return next, removed
}

func normalizeHosts(values []string) ([]string, error) {
	seen := make(map[string]bool, len(values))
	hosts := make([]string, 0, len(values))
	for _, value := range values {
		host := strings.TrimSpace(value)
		if err := validateHost(host); err != nil {
			return nil, err
		}
		if seen[host] {
			continue
		}
		seen[host] = true
		hosts = append(hosts, host)
	}
	return hosts, nil
}

func validateHost(host string) error {
	if host == "" {
		return errors.New("host cannot be empty")
	}
	if strings.ContainsAny(host, " \t\r\n#") {
		return fmt.Errorf("invalid host %q", host)
	}
	return nil
}
