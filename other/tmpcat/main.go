package main

import (
	"fmt"
	"time"

	"go-tools/tools/php"
)

func main() {
	ver := "7.4.33"
	cfg := php.InstanceConfig{Name: "tmpext2", Version: ver, NTS: true, Port: 18096}
	if err := php.SaveInstance(cfg); err != nil {
		fmt.Println("save:", err)
		return
	}
	list, _ := php.ListInstances()
	var id string
	for _, c := range list {
		if c.Name == "tmpext2" {
			id = c.ID
		}
	}
	defer php.RemoveInstance(id)
	for _, force := range []bool{true, false} {
		t0 := time.Now()
		cat, err := php.ExtCatalog(id, force)
		fmt.Println("=== first call force:", force, "elapsed:", time.Since(t0), "err:", err)
		for {
			fetching := 0
			for _, c := range cat {
				if c.Fetching {
					fetching++
				}
			}
			if fetching == 0 || time.Since(t0) > 90*time.Second {
				break
			}
			time.Sleep(700 * time.Millisecond)
			cat, err = php.ExtCatalog(id, false)
			if err != nil {
				break
			}
		}
		fmt.Println("=== ready in", time.Since(t0))
		for _, c := range cat {
			fmt.Printf("  %-10s match=%-5v ver=%-8s size=%-8d reason=%s\n", c.Name, c.Matchable, c.ExtVer, c.Size, c.Reason)
		}
	}
}
