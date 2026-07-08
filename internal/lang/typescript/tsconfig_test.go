package typescript

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadTSConfigBasic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tsconfig.json"), `{
  "compilerOptions": {
    "baseUrl": "./src",
    "paths": {
      "@app/*": ["*"],
      "@lib": ["lib/index.ts"]
    }
  }
}`)
	cfg, err := loadTSConfig(root)
	if err != nil {
		t.Fatalf("loadTSConfig: %v", err)
	}
	if cfg.BaseURL != "./src" {
		t.Errorf("BaseURL = %q, want ./src", cfg.BaseURL)
	}
	if !reflect.DeepEqual(cfg.Paths["@app/*"], []string{"*"}) {
		t.Errorf("paths[@app/*] = %v", cfg.Paths["@app/*"])
	}
	if !reflect.DeepEqual(cfg.Paths["@lib"], []string{"lib/index.ts"}) {
		t.Errorf("paths[@lib] = %v", cfg.Paths["@lib"])
	}
}

func TestLoadTSConfigJSONC(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tsconfig.json"), `{
  // a line comment
  "compilerOptions": {
    "baseUrl": ".", /* inline block */
    "paths": {
      "@x/*": ["x/*"], // trailing comment
    }, // trailing comma above and here
  },
}`)
	cfg, err := loadTSConfig(root)
	if err != nil {
		t.Fatalf("loadTSConfig with JSONC: %v", err)
	}
	if cfg.BaseURL != "." {
		t.Errorf("BaseURL = %q, want .", cfg.BaseURL)
	}
	if !reflect.DeepEqual(cfg.Paths["@x/*"], []string{"x/*"}) {
		t.Errorf("paths[@x/*] = %v", cfg.Paths["@x/*"])
	}
}

func TestLoadTSConfigMissingIsEmpty(t *testing.T) {
	root := t.TempDir()
	cfg, err := loadTSConfig(root)
	if err != nil {
		t.Fatalf("missing tsconfig should not error: %v", err)
	}
	if cfg == nil {
		t.Fatal("cfg is nil, want empty config")
	}
	if cfg.BaseURL != "" || len(cfg.Paths) != 0 {
		t.Errorf("empty config expected, got %+v", cfg)
	}
}

func TestLoadTSConfigExtends(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tsconfig.base.json"), `{
  "compilerOptions": {
    "baseUrl": "./src",
    "paths": { "@base/*": ["base/*"] }
  }
}`)
	writeFile(t, filepath.Join(root, "tsconfig.json"), `{
  "extends": "./tsconfig.base.json",
  "compilerOptions": {
    "paths": { "@app/*": ["app/*"] }
  }
}`)
	cfg, err := loadTSConfig(root)
	if err != nil {
		t.Fatalf("loadTSConfig extends: %v", err)
	}
	// baseUrl inherited from base; child paths override/merge in.
	if cfg.BaseURL != "./src" {
		t.Errorf("BaseURL = %q, want inherited ./src", cfg.BaseURL)
	}
	if !reflect.DeepEqual(cfg.Paths["@base/*"], []string{"base/*"}) {
		t.Errorf("inherited paths[@base/*] = %v", cfg.Paths["@base/*"])
	}
	if !reflect.DeepEqual(cfg.Paths["@app/*"], []string{"app/*"}) {
		t.Errorf("own paths[@app/*] = %v", cfg.Paths["@app/*"])
	}
}

func TestPackageName(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{ "name": "my-pkg", "version": "1.0.0" }`)
	if got := packageName(root); got != "my-pkg" {
		t.Errorf("packageName = %q, want my-pkg", got)
	}
}

func TestPackageNameFallsBackToBasename(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myproject")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// no package.json
	if got := packageName(root); got != "myproject" {
		t.Errorf("packageName without package.json = %q, want myproject", got)
	}
	// package.json without a name
	writeFile(t, filepath.Join(root, "package.json"), `{ "version": "1.0.0" }`)
	if got := packageName(root); got != "myproject" {
		t.Errorf("packageName with unnamed package.json = %q, want myproject", got)
	}
}
