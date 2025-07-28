package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

type modInfo struct {
	Path      string `json:"Path"`  // module name
	Dir       string `json:"Dir"`   // absolute path to the module
	GoMod     string `json:"GoMod"` // absolute path to the go.mod
	GoVersion string `json:"GoVersion"`
	Main      bool   `json:"Main"`
}

func getModuleInfo(ctx context.Context, dir string) (modInfo, error) {
	cmd := exec.CommandContext(ctx, "go", "env", "-json", "GOMOD")
	if dir != "" {
		cmd.Dir = dir
	}

	out, err := cmd.Output()
	if err != nil {
		return modInfo{}, fmt.Errorf("command %q: %w: %s", strings.Join(cmd.Args, " "), err, string(out))
	}

	v := map[string]string{}

	err = json.NewDecoder(bytes.NewBuffer(out)).Decode(&v)
	if err != nil {
		return modInfo{}, err
	}

	goModPath := v["GOMOD"]

	data, err := os.ReadFile(goModPath)
	if err != nil {
		return modInfo{}, err
	}

	goModFile, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		return modInfo{}, err
	}

	return modInfo{
		Path:      goModFile.Module.Mod.Path,
		Dir:       filepath.Dir(goModPath),
		GoMod:     goModPath,
		GoVersion: goModFile.Go.Version,
		Main:      true,
	}, nil
}
