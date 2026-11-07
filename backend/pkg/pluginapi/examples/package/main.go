// Command package builds independent example packages. It never builds the host.
package main

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type signingKey struct {
	Private string `json:"private_key"`
	Public  string `json:"public_key"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	kind := flag.String("kind", "", "frontend, backend, combined, or legacy")
	version := flag.String("version", "1.0.0", "example package version")
	hostRange := flag.String("host-range", ">=0.0.0", "required host version range")
	emailGrant := flag.Bool("email-grant", false, "explicitly request current email access for backend examples")
	out := flag.String("out-dir", "", "output directory (required)")
	keyFile := flag.String("sign-key-file", "", "local Ed25519 JSON key file; contents are never printed")
	publisher := flag.String("publisher", "local-example", "trusted publisher key ID")
	unsigned := flag.Bool("dev-unsigned", false, "explicit isolated development use only")
	generateKey := flag.String("generate-test-key", "", "create a new local Ed25519 JSON key file and exit")
	flag.Parse()
	if *generateKey != "" {
		pub, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(signingKey{Private: base64.StdEncoding.EncodeToString(private), Public: base64.StdEncoding.EncodeToString(pub)})
		if err != nil {
			return err
		}
		file, err := os.OpenFile(*generateKey, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return errors.New("cannot create new test key file (existing files are never overwritten)")
		}
		if _, err = file.Write(raw); err != nil {
			_ = file.Close()
			return err
		}
		return file.Close()
	}
	if *out == "" || (*kind != "frontend" && *kind != "backend" && *kind != "combined" && *kind != "legacy") {
		return errors.New("--kind and --out-dir are required")
	}
	if strings.ContainsAny(*version, "/\\ \t\n") || *version == "" {
		return errors.New("invalid package version")
	}
	if *keyFile == "" && !*unsigned {
		return errors.New("signed packages require --sign-key-file; default signature rules are unchanged")
	}
	if *emailGrant && (*kind == "frontend" || *kind == "legacy") {
		return errors.New("--email-grant is only supported by the generic backend examples")
	}
	moduleRoot, err := os.Getwd()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(moduleRoot, "go.mod")); err != nil {
		return errors.New("run the packager from the backend module directory")
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		return err
	}
	files := map[string][]byte{}
	caps := []any{}
	capIDs := []string{}
	requires := map[string]any{"sub2api": *hostRange, "plugin_protocol": 0, "transport_api": 0, "backend_api": 0, "host_service_api": 0, "ui_bridge": 0}
	manifest := map[string]any{"schema_version": 2, "id": "example." + *kind, "name": "独立插件示例 / " + *kind, "version": *version, "description": "公开插件能力的独立运行验证，不包含真实提醒或奖励", "requires": requires, "capabilities": caps, "runtimes": map[string]any{}}
	if *kind != "backend" {
		requires["ui_bridge"] = 2
		entries := []any{}
		for _, mount := range []string{"user", "admin"} {
			roles := []string{"user", "admin"}
			if mount == "admin" {
				roles = []string{"admin"}
			}
			entries = append(entries, map[string]any{"id": mount, "label": "示例 " + *kind + " / " + mount, "path": "ui/index.html", "mount": mount, "roles": roles})
		}
		if *kind == "frontend" {
			entries = append(entries, map[string]any{"id": "helper", "label": "独立辅助面板", "path": "ui/index.html", "mount": "shell", "roles": []string{"user", "admin"}})
		}
		manifest["ui"] = map[string]any{"entries": entries}
		index, err := os.ReadFile(filepath.Join(moduleRoot, "pkg/pluginapi/examples", *kind, "ui/index.html"))
		if err != nil {
			return err
		}
		bridge, err := os.ReadFile(filepath.Join(moduleRoot, "pkg/pluginapi/examples/common/bridge.js"))
		if err != nil {
			return err
		}
		files["ui/index.html"], files["ui/bridge.js"] = index, bridge
	}
	if *kind != "frontend" {
		if *kind == "legacy" {
			manifest["schema_version"] = 1
			delete(requires, "backend_api")
			delete(requires, "host_service_api")
			requires["plugin_protocol"], requires["transport_api"], requires["ui_bridge"] = 1, 1, 1
			manifest["ui"] = map[string]any{"entrypoint": "ui/index.html"}
			manifest["capabilities"] = []any{map[string]any{"id": "openai.oauth.outbound_transport.v1", "platform": "openai", "account_type": "oauth"}}
		} else {
			requires["plugin_protocol"], requires["backend_api"], requires["host_service_api"] = 1, 1, 3
			capIDs = []string{"backend.invoke.v1", "backend.background.v1", "storage.read.v1", "storage.write.v1", "users.created.read.v1"}
			if *emailGrant {
				capIDs = append(capIDs, "users.email.read.v1")
			}
			for _, id := range capIDs {
				caps = append(caps, map[string]any{"id": id})
			}
			manifest["capabilities"] = caps
			methods := []any{}
			for _, item := range []struct{ name, role, access string }{{"echo", "user", "read"}, {"read_note", "user", "read"}, {"write_note", "user", "write"}, {"host_probe", "user", "read"}, {"event_list", "admin", "read"}, {"wait", "admin", "read"}, {"crash", "admin", "write"}} {
				methods = append(methods, map[string]any{"name": item.name, "role": item.role, "access": item.access})
			}
			manifest["backend"] = map[string]any{"methods": methods}
		}
		binaryPath := filepath.Join(*out, "example-"+*kind+"-runtime")
		args := []string{"build", "-trimpath", "-o", binaryPath}
		if *kind == "legacy" {
			args = append(args, "-ldflags=-s -w -X main.pluginVersion="+*version)
		} else {
			pkg := "github.com/Wei-Shaw/sub2api/pkg/pluginapi/examples/internal/demo."
			args = append(args, "-ldflags=-s -w -X "+pkg+"PluginID=example."+*kind+" -X "+pkg+"PluginVersion="+*version+" -X "+pkg+"CapabilitiesCSV="+strings.Join(capIDs, ","))
		}
		args = append(args, "./pkg/pluginapi/examples/"+*kind)
		command := exec.Command("go", args...)
		command.Env = append(os.Environ(), "CGO_ENABLED=0")
		command.Stdout, command.Stderr = os.Stdout, os.Stderr
		if err := command.Run(); err != nil {
			return fmt.Errorf("build independent example: %w", err)
		}
		binary, err := os.ReadFile(binaryPath)
		if err != nil {
			return err
		}
		files["bin/plugin"] = binary
		manifest["runtimes"] = map[string]any{runtime.GOOS + "-" + runtime.GOARCH: map[string]any{"path": "bin/plugin"}}
	}
	hashes := map[string]string{}
	for name, data := range files {
		digest := sha256.Sum256(data)
		hashes[name] = hex.EncodeToString(digest[:])
	}
	manifest["files"] = hashes
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	files["manifest.json"] = raw
	if *keyFile != "" {
		keyRaw, err := os.ReadFile(*keyFile)
		if err != nil {
			return errors.New("cannot read signing key file")
		}
		var key signingKey
		if json.Unmarshal(keyRaw, &key) != nil {
			return errors.New("invalid signing key file")
		}
		private, err := base64.StdEncoding.DecodeString(key.Private)
		if err != nil || len(private) != ed25519.PrivateKeySize {
			return errors.New("invalid Ed25519 signing key")
		}
		signature, err := json.Marshal(map[string]string{"algorithm": "ed25519", "key_id": *publisher, "signature": base64.StdEncoding.EncodeToString(ed25519.Sign(private, raw))})
		if err != nil {
			return err
		}
		files["signature.json"] = signature
	}
	file, err := os.OpenFile(filepath.Join(*out, "example-"+*kind+"-"+*version+".s2plugin"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	paths := make([]string, 0, len(files))
	for name := range files {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	for _, name := range paths {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0o600)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		if _, err := entry.Write(files[name]); err != nil {
			return err
		}
	}
	return writer.Close()
}
