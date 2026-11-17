package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Container is one row of the containers page.
type Container struct {
	Name     string
	Rootless bool
	Running  bool
	State    string
	MemMB    float64
}

// podmanClient talks to the Podman REST API on a unix socket (no podman CLI).
type podmanClient struct {
	sock     string
	rootless bool
	hc       *http.Client
}

func newPodman(sock string, rootless bool) *podmanClient {
	return &podmanClient{sock: sock, rootless: rootless, hc: &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		}},
	}}
}

func (p *podmanClient) get(path string, v interface{}) error {
	resp, err := p.hc.Get("http://d/v4.0.0/libpod" + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

func (p *podmanClient) list() []Container {
	var cs []struct {
		Names []string
		State string
	}
	if err := p.get("/containers/json?all=true", &cs); err != nil {
		return nil
	}
	var st struct {
		Stats []struct {
			Name     string
			MemUsage uint64
		}
	}
	mem := map[string]float64{}
	if p.get("/containers/stats?stream=false", &st) == nil {
		for _, s := range st.Stats {
			mem[s.Name] = float64(s.MemUsage) / (1 << 20)
		}
	}
	out := make([]Container, 0, len(cs))
	for _, c := range cs {
		name := "?"
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		out = append(out, Container{
			Name: name, Rootless: p.rootless, Running: c.State == "running",
			State: c.State, MemMB: mem[name],
		})
	}
	return out
}

// containers lists root and rootless (every /run/user/*/podman) containers,
// running ones first.
func containers() []Container {
	socks := []*podmanClient{newPodman("/run/podman/podman.sock", false)}
	users, _ := filepath.Glob("/run/user/*/podman/podman.sock")
	for _, s := range users {
		socks = append(socks, newPodman(s, true))
	}
	var all []Container
	for _, p := range socks {
		all = append(all, p.list()...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Running != all[j].Running {
			return all[i].Running
		}
		return all[i].Name < all[j].Name
	})
	return all
}
