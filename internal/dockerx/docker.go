package dockerx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type Usage struct {
	Type        string
	Total       int
	Active      int
	Size        string
	Reclaimable string
}

type Container struct {
	ID     string
	Name   string
	Image  string
	Status string
	Size   string
}

type Summary struct {
	Installed         bool
	Running           bool
	Usage             []Usage
	StoppedContainers []Container
	UnusedNetworks    int
}

type Category int

const (
	StoppedContainers Category = iota
	UnusedImages
	BuildCache
	UnusedNetworks
)

func Installed() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}

func Scan() (Summary, error) {
	s := Summary{Installed: Installed()}
	if !s.Installed {
		return s, nil
	}

	if err := exec.Command("docker", "info").Run(); err != nil {
		return s, nil
	}
	s.Running = true

	usage, err := systemDF()
	if err != nil {
		return s, err
	}
	s.Usage = usage

	containers, err := stoppedContainers()
	if err != nil {
		return s, err
	}
	s.StoppedContainers = containers

	networks, _ := unusedNetworkCount()
	s.UnusedNetworks = networks
	return s, nil
}

func systemDF() ([]Usage, error) {
	cmd := exec.Command("docker", "system", "df", "--format", "{{json .}}")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker system df failed: %w", err)
	}

	var rows []Usage
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		rows = append(rows, Usage{
			Type:        stringValue(raw["Type"]),
			Total:       intValue(raw["TotalCount"]),
			Active:      intValue(raw["Active"]),
			Size:        stringValue(raw["Size"]),
			Reclaimable: stringValue(raw["Reclaimable"]),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

func stoppedContainers() ([]Container, error) {
	args := []string{
		"ps", "-a", "--size",
		"--filter", "status=exited",
		"--filter", "status=created",
		"--filter", "status=dead",
		"--format", "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Size}}",
	}
	out, err := exec.Command("docker", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("docker ps failed: %w", err)
	}
	var result []Container
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 5 {
			continue
		}
		result = append(result, Container{ID: parts[0], Name: parts[1], Image: parts[2], Status: parts[3], Size: parts[4]})
	}
	return result, nil
}

func unusedNetworkCount() (int, error) {
	out, err := exec.Command("docker", "network", "ls", "--filter", "dangling=true", "--format", "{{.ID}}").Output()
	if err != nil {
		return 0, err
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return 0, nil
	}
	return len(strings.Split(text, "\n")), nil
}

func Prune(categories []Category) (string, error) {
	if !Installed() {
		return "", errors.New("docker is not installed or not in PATH")
	}
	var output strings.Builder
	for _, category := range categories {
		var args []string
		var label string
		switch category {
		case StoppedContainers:
			label = "Stopped containers"
			args = []string{"container", "prune", "-f"}
		case UnusedImages:
			label = "Unused images"
			args = []string{"image", "prune", "-a", "-f"}
		case BuildCache:
			label = "Build cache"
			args = []string{"builder", "prune", "-a", "-f"}
		case UnusedNetworks:
			label = "Unused networks"
			args = []string{"network", "prune", "-f"}
		default:
			continue
		}
		out, err := exec.Command("docker", args...).CombinedOutput()
		if err != nil {
			return output.String(), fmt.Errorf("%s cleanup failed: %w: %s", label, err, strings.TrimSpace(string(out)))
		}
		output.WriteString(label)
		output.WriteString(":\n")
		output.Write(out)
		if len(out) > 0 && out[len(out)-1] != '\n' {
			output.WriteByte('\n')
		}
	}
	return output.String(), nil
}

func UsageByType(rows []Usage, name string) Usage {
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSpace(row.Type), name) {
			return row
		}
	}
	return Usage{Type: name, Size: "—", Reclaimable: "—"}
}

func intValue(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	case json.Number:
		i, _ := strconv.Atoi(n.String())
		return i
	default:
		return 0
	}
}

func stringValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
