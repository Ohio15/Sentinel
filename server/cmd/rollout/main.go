// Sentinel rollout CLI — wraps the POST/GET/cancel /api/rollouts endpoints
// for ops scripts and cron-driven release campaigns.
//
// Usage:
//
//	sentinel-rollout create --release 1.77.10 --target all-online
//	sentinel-rollout create --release 1.77.10 --target device-list --device-ids uuid1,uuid2
//	sentinel-rollout list [--status active]
//	sentinel-rollout get <id>
//	sentinel-rollout cancel <id>
//
// Auth: SENTINEL_API_KEY env var (X-API-Key header).
// Endpoint: SENTINEL_API_BASE env var, defaults to https://localhost:8080.
package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const usage = `sentinel-rollout — Sentinel rollout pipeline CLI

Subcommands:
  create   Create a new rollout
  list     List rollouts (optional --status filter)
  get      Show one rollout in detail
  cancel   Cancel an active or pending rollout

Environment:
  SENTINEL_API_BASE   Backend base URL (default: https://localhost:8080)
  SENTINEL_API_KEY    Required. X-API-Key header value.
  SENTINEL_INSECURE   If "1", skip TLS verification (for local dev / self-signed certs).

Run "sentinel-rollout <subcommand> --help" for subcommand flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	apiKey := os.Getenv("SENTINEL_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "error: SENTINEL_API_KEY is not set")
		os.Exit(2)
	}
	base := os.Getenv("SENTINEL_API_BASE")
	if base == "" {
		base = "https://localhost:8080"
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: os.Getenv("SENTINEL_INSECURE") == "1",
			},
		},
	}

	switch os.Args[1] {
	case "create":
		cmdCreate(client, base, apiKey, os.Args[2:])
	case "list":
		cmdList(client, base, apiKey, os.Args[2:])
	case "get":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "error: get requires <id>")
			os.Exit(2)
		}
		cmdGet(client, base, apiKey, os.Args[2])
	case "cancel":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "error: cancel requires <id>")
			os.Exit(2)
		}
		cmdCancel(client, base, apiKey, os.Args[2])
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}

func cmdCreate(client *http.Client, base, apiKey string, args []string) {
	fs := flag.NewFlagSet("create", flag.ExitOnError)
	release := fs.String("release", "", "Release version (must exist in agent_releases). Required.")
	target := fs.String("target", "all-online", "Target type: all-online | device-list")
	deviceIDs := fs.String("device-ids", "", "Comma-separated UUIDs (required when --target=device-list)")
	name := fs.String("name", "", "Optional human-readable name")
	description := fs.String("description", "", "Optional description")
	failureThreshold := fs.Float64("failure-threshold", 20.0, "Rollout fails if more than this %% of devices fail")
	_ = fs.Parse(args)

	if *release == "" {
		fmt.Fprintln(os.Stderr, "error: --release is required")
		os.Exit(2)
	}
	if *target != "all-online" && *target != "device-list" {
		fmt.Fprintln(os.Stderr, "error: --target must be all-online or device-list")
		os.Exit(2)
	}

	body := map[string]interface{}{
		"release_version":           *release,
		"mode":                      "immediate",
		"channel":                   "stable",
		"target":                    map[string]interface{}{"type": *target},
		"failure_threshold_percent": *failureThreshold,
	}
	if *name != "" {
		body["name"] = *name
	}
	if *description != "" {
		body["description"] = *description
	}
	if *target == "device-list" {
		if *deviceIDs == "" {
			fmt.Fprintln(os.Stderr, "error: --device-ids required when --target=device-list")
			os.Exit(2)
		}
		ids := strings.Split(*deviceIDs, ",")
		for i := range ids {
			ids[i] = strings.TrimSpace(ids[i])
		}
		body["target"].(map[string]interface{})["device_ids"] = ids
	}

	doRequest(client, "POST", base+"/api/rollouts", apiKey, body)
}

func cmdList(client *http.Client, base, apiKey string, args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	status := fs.String("status", "", "Filter by status (csv ok)")
	limit := fs.Int("limit", 50, "Max rollouts to return (max 200)")
	_ = fs.Parse(args)

	q := url.Values{}
	if *status != "" {
		q.Set("status", *status)
	}
	q.Set("limit", fmt.Sprintf("%d", *limit))
	doRequest(client, "GET", base+"/api/rollouts?"+q.Encode(), apiKey, nil)
}

func cmdGet(client *http.Client, base, apiKey, id string) {
	doRequest(client, "GET", base+"/api/rollouts/"+id, apiKey, nil)
}

func cmdCancel(client *http.Client, base, apiKey, id string) {
	doRequest(client, "POST", base+"/api/rollouts/"+id+"/cancel", apiKey, nil)
}

func doRequest(client *http.Client, method, url, apiKey string, body interface{}) {
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: marshal body: %v\n", err)
			os.Exit(1)
		}
		buf = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, buf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: build request: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("X-API-Key", apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: request failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		fmt.Fprintf(os.Stderr, "error: HTTP %d\n%s\n", resp.StatusCode, string(respBody))
		os.Exit(1)
	}

	if len(respBody) > 0 {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, respBody, "", "  "); err == nil {
			fmt.Println(pretty.String())
		} else {
			fmt.Println(string(respBody))
		}
	}
}
