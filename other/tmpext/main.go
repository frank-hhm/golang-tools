package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go-tools/tools/php"
)

func main() {
	ver := "7.4.33"
	cfg := php.InstanceConfig{Name: "tmpext", Version: ver, NTS: true, Port: 18097}
	if err := php.SaveInstance(cfg); err != nil {
		fmt.Println("save:", err)
		return
	}
	list, _ := php.ListInstances()
	var id string
	for _, c := range list {
		if c.Name == "tmpext" {
			id = c.ID
		}
	}
	defer php.RemoveInstance(id)

	php.SetEmitter(func(name string, data ...any) {
		fmt.Println("EVENT", name, fmt.Sprintf("%+v", data))
	})

	t0 := time.Now()
	cat, err := php.ExtCatalog(id, true)
	fmt.Println("first call in", time.Since(t0), "err:", err)
	for i := 0; i < 60; i++ {
		fetching := 0
		for _, c := range cat {
			if c.Fetching {
				fetching++
			}
		}
		if fetching == 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
		cat, err = php.ExtCatalog(id, false)
	}
	fmt.Println("ready in", time.Since(t0))
	for _, c := range cat {
		fmt.Printf("  %-10s inst=%-5v match=%-5v seen=%-5v ver=%-8s size=%-8d avail=[%s] reason=%s\n",
			c.Name, c.Installed, c.Matchable, c.SeriesSeen, c.ExtVer, c.Size, c.Available, c.Reason)
	}

	fmt.Println("--- install xdebug (deep lookup, PHP 7.4) ---")
	t1 := time.Now()
	if err := php.InstallExtension(id, "xdebug", true); err != nil {
		fmt.Println("install err:", err)
	} else {
		for i := 0; i < 240 && php.ExtensionInstalling("xdebug"); i++ {
			time.Sleep(500 * time.Millisecond)
		}
		fmt.Println("install finished in", time.Since(t1))
	}
	exe := ""
	vers, _ := php.ListVersions()
	for _, v := range vers {
		if v.Version == ver {
			exe = v.Path
		}
	}
	dll := filepath.Join(filepath.Dir(exe), "ext", "php_xdebug.dll")
	if st, err := os.Stat(dll); err == nil {
		fmt.Println("dll ok:", dll, st.Size())
	} else {
		fmt.Println("dll MISSING:", err)
	}
	if _, err := os.Stat(dll); err == nil {
		fmt.Println("remove err:", php.RemoveExtension(id, "xdebug"))
	}
}
