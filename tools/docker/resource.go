package docker

// ============================================================
// Docker 管理 - 网络 / 存储卷 / 编排（Windows 实现）
//
// 网络与存储卷直连 Engine API 的 /networks、/volumes；
// 「编排」在 Engine API 里没有对应端点（compose 是 CLI 的活），
// 这里按容器标签 com.docker.compose.* 把容器归组成项目，
// 只做归组展示与批量启停，不解析 yml、不依赖 docker CLI。
// ============================================================

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"Tools/services/applog"
)

/* ============================ 公共 ============================ */

// 网络 / 卷名与容器名同一套字符集：首字符字母数字，其后允许 _ . -
var resNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)

func checkResName(what, name string) error {
	if name == "" {
		return fmt.Errorf("%s不能为空", what)
	}
	if !resNameRe.MatchString(name) {
		return fmt.Errorf("%s只能包含字母、数字、下划线、点、短横线，且以字母或数字开头（最长 64 字符）", what)
	}
	return nil
}

/* ============================ 网络 ============================ */

// ListNetworks 列出网络（带 1.5 秒缓存，与容器列表同一套闸门/熔断）。
// 内置网络 bridge/host/none 标 Builtin：daemon 不让删，前端据此禁按钮
func ListNetworks() ([]Network, error) {
	return memoRead("networks", 1500*time.Millisecond, func() ([]Network, error) {
		var raw []struct {
			Name       string            `json:"Name"`
			ID         string            `json:"Id"`
			Created    string            `json:"Created"`
			Scope      string            `json:"Scope"`
			Driver     string            `json:"Driver"`
			Internal   bool              `json:"Internal"`
			Attachable bool              `json:"Attachable"`
			Ingress    bool              `json:"Ingress"`
			Labels     map[string]string `json:"Labels"`
			IPAM       struct {
				Config []struct {
					Subnet  string `json:"Subnet"`
					Gateway string `json:"Gateway"`
				} `json:"Config"`
			} `json:"IPAM"`
			Containers map[string]struct {
				Name string `json:"Name"`
			} `json:"Containers"`
		}
		if err := do(http.MethodGet, "/networks", nil, nil, &raw); err != nil {
			return nil, err
		}
		out := make([]Network, 0, len(raw))
		for _, r := range raw {
			n := Network{
				ID:         shortID(r.ID),
				Name:       r.Name,
				Driver:     r.Driver,
				Scope:      r.Scope,
				Internal:   r.Internal,
				Attachable: r.Attachable,
				Ingress:    r.Ingress,
				Created:    r.Created,
				Containers: len(r.Containers),
				Labels:     r.Labels,
				OwnerTool:  r.Labels[LabelTool],
				Builtin:    r.Name == "bridge" || r.Name == "host" || r.Name == "none",
			}
			// 一个网络可能有多个 IPAM 子网（多网段），列表只展示第一个
			for _, c := range r.IPAM.Config {
				if c.Subnet != "" {
					n.Subnet, n.Gateway = c.Subnet, c.Gateway
					break
				}
			}
			out = append(out, n)
		}
		sort.Slice(out, func(i, j int) bool {
			// 内置网络沉底：它们是 daemon 自带的，不该排在自建网络前面
			if out[i].Builtin != out[j].Builtin {
				return !out[i].Builtin
			}
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		})
		return out, nil
	})
}

// CreateNetwork 新建自定义网络；driver 空走 bridge，subnet 空交给 IPAM
func CreateNetwork(spec NetworkSpec) (string, error) {
	name := strings.TrimSpace(spec.Name)
	if err := checkResName("网络名称", name); err != nil {
		return "", err
	}
	body := map[string]any{"Name": name, "CheckDuplicate": true}
	if d := strings.TrimSpace(spec.Driver); d != "" {
		body["Driver"] = d
	}
	if spec.Internal {
		body["Internal"] = true
	}
	if sub := strings.TrimSpace(spec.Subnet); sub != "" {
		// 子网写错时 daemon 会在 warning/message 里说清楚，这里只管原样送过去
		body["IPAM"] = map[string]any{
			"Driver": "default",
			"Config": []map[string]string{{"Subnet": sub}},
		}
	}
	var out struct {
		ID      string `json:"Id"`
		Warning string `json:"Warning"`
	}
	if err := do(http.MethodPost, "/networks/create", nil, body, &out); err != nil {
		return "", err
	}
	if out.Warning != "" {
		applog.Warnf("docker", "创建网络 %s 有告警：%s", name, out.Warning)
	}
	applog.Infof("docker", "已创建网络 %s（%s）", name, shortID(out.ID))
	return out.ID, nil
}

// RemoveNetwork 删除网络。仍有容器接入时 daemon 回 409，这里翻成可执行的下一步
func RemoveNetwork(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("网络 ID 不能为空")
	}
	err := do(http.MethodDelete, "/networks/"+url.PathEscape(id), nil, nil, nil)
	if err != nil {
		if strings.Contains(err.Error(), "active endpoints") {
			return fmt.Errorf("网络里还有容器在接入，先停止或断开这些容器再删：%v", err)
		}
		return err
	}
	applog.Infof("docker", "已删除网络 %s", id)
	return nil
}

/* ============================ 存储卷 ============================ */

// volumeRefs 统计每个具名卷被多少容器引用。
// daemon 的 /volumes 不带引用关系，只能靠容器列表里的 Mounts 现算；
// bind 挂载没有 Name，不计入
func volumeRefs() map[string]int {
	use, _ := memoRead("volume-refs", 2*time.Second, func() (map[string]int, error) {
		var raw []struct {
			Mounts []struct {
				Type string `json:"Type"`
				Name string `json:"Name"`
			} `json:"Mounts"`
		}
		if err := do(http.MethodGet, "/containers/json", url.Values{"all": {"true"}}, nil, &raw); err != nil {
			return map[string]int{}, err
		}
		m := make(map[string]int, len(raw))
		for _, c := range raw {
			for _, mt := range c.Mounts {
				if mt.Type == "volume" && mt.Name != "" {
					m[mt.Name]++
				}
			}
		}
		return m, nil
	})
	if use == nil {
		use = map[string]int{}
	}
	return use
}

// ListVolumes 列出存储卷（带 1.5 秒缓存）；InUse 由 volumeRefs 现算
func ListVolumes() ([]Volume, error) {
	return memoRead("volumes", 1500*time.Millisecond, func() ([]Volume, error) {
		var raw struct {
			Volumes []struct {
				CreatedAt  string            `json:"CreatedAt"`
				Driver     string            `json:"Driver"`
				Labels     map[string]string `json:"Labels"`
				Mountpoint string            `json:"Mountpoint"`
				Name       string            `json:"Name"`
				Scope      string            `json:"Scope"`
			} `json:"Volumes"`
		}
		if err := do(http.MethodGet, "/volumes", nil, nil, &raw); err != nil {
			return nil, err
		}
		use := volumeRefs()
		out := make([]Volume, 0, len(raw.Volumes))
		for _, v := range raw.Volumes {
			out = append(out, Volume{
				Name:       v.Name,
				Driver:     v.Driver,
				Mountpoint: v.Mountpoint,
				Created:    v.CreatedAt,
				Scope:      v.Scope,
				InUse:      use[v.Name],
				Labels:     v.Labels,
				OwnerTool:  v.Labels[LabelTool],
			})
		}
		sort.Slice(out, func(i, j int) bool {
			// 有容器在用的排前面：这些才是正在产生数据的卷
			if (out[i].InUse > 0) != (out[j].InUse > 0) {
				return out[i].InUse > 0
			}
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		})
		return out, nil
	})
}

// CreateVolume 新建存储卷
func CreateVolume(spec VolumeSpec) (string, error) {
	name := strings.TrimSpace(spec.Name)
	if err := checkResName("存储卷名称", name); err != nil {
		return "", err
	}
	body := map[string]any{"Name": name}
	if d := strings.TrimSpace(spec.Driver); d != "" {
		body["Driver"] = d
	}
	var out struct {
		Name string `json:"Name"`
	}
	if err := do(http.MethodPost, "/volumes/create", nil, body, &out); err != nil {
		return "", err
	}
	applog.Infof("docker", "已创建存储卷 %s", name)
	return out.Name, nil
}

// RemoveVolume 删除存储卷；仍被容器占用时 daemon 回 409
func RemoveVolume(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("存储卷名称不能为空")
	}
	err := do(http.MethodDelete, "/volumes/"+url.PathEscape(name), nil, nil, nil)
	if err != nil {
		if strings.Contains(err.Error(), "in use") {
			return fmt.Errorf("卷仍被容器占用，先停止并删除使用它的容器：%v", err)
		}
		return err
	}
	applog.Infof("docker", "已删除存储卷 %s", name)
	return nil
}

// PruneVolumes 清理没有任何容器引用的存储卷
func PruneVolumes() (VolumePruneReport, error) {
	var out struct {
		VolumesDeleted []string `json:"VolumesDeleted"`
		SpaceReclaimed int64    `json:"SpaceReclaimed"`
	}
	if err := do(http.MethodPost, "/volumes/prune", nil, nil, &out); err != nil {
		return VolumePruneReport{}, err
	}
	applog.Infof("docker", "清理未使用存储卷 %d 个，回收 %d 字节", len(out.VolumesDeleted), out.SpaceReclaimed)
	return VolumePruneReport{Deleted: out.VolumesDeleted, Space: out.SpaceReclaimed}, nil
}

/* ============================ 编排 ============================ */

// compose 写在容器标签里的元数据（docker compose 自己打的，与版本无关）
const (
	composeProject = "com.docker.compose.project"
	composeService = "com.docker.compose.service"
	composeWorkDir = "com.docker.compose.project.working_dir"
	composeConfigs = "com.docker.compose.project.config_files"
)

// ListComposeProjects 按 compose 标签把容器归组成项目（带 1.5 秒缓存）。
// 没有 compose 标签的容器直接跳过——它们属于「容器」页，不属于编排
func ListComposeProjects() ([]ComposeProject, error) {
	return memoRead("compose-projects", 1500*time.Millisecond, func() ([]ComposeProject, error) {
		var raw []struct {
			ID      string            `json:"Id"`
			Names   []string          `json:"Names"`
			Image   string            `json:"Image"`
			Created int64             `json:"Created"`
			State   string            `json:"State"`
			Status  string            `json:"Status"`
			Labels  map[string]string `json:"Labels"`
		}
		if err := do(http.MethodGet, "/containers/json", url.Values{"all": {"true"}}, nil, &raw); err != nil {
			return nil, err
		}
		idx := make(map[string]*ComposeProject, 4)
		for _, c := range raw {
			pname := strings.TrimSpace(c.Labels[composeProject])
			if pname == "" {
				continue
			}
			p := idx[pname]
			if p == nil {
				p = &ComposeProject{Name: pname, WorkingDir: c.Labels[composeWorkDir]}
				for _, f := range strings.Split(c.Labels[composeConfigs], ",") {
					if f = strings.TrimSpace(f); f != "" {
						p.ConfigFiles = append(p.ConfigFiles, f)
					}
				}
				idx[pname] = p
			}
			name := ""
			if len(c.Names) > 0 {
				name = strings.TrimPrefix(c.Names[0], "/")
			}
			svc := ComposeService{
				Name:      strings.TrimSpace(c.Labels[composeService]),
				Container: name,
				ID:        shortID(c.ID),
				Image:     c.Image,
				State:     c.State,
				Status:    c.Status,
			}
			if svc.Name == "" {
				svc.Name = name
			}
			p.Services = append(p.Services, svc)
			p.Total++
			if c.State == "running" {
				p.Running++
			}
			// 项目创建时间取最早的那个容器（重建后新容器会更新，但列表里差几秒无意义）
			if p.Created == 0 || c.Created < p.Created {
				p.Created = c.Created
			}
		}
		out := make([]ComposeProject, 0, len(idx))
		for _, p := range idx {
			switch {
			case p.Total > 0 && p.Running == p.Total:
				p.Status = "running"
			case p.Running > 0:
				p.Status = "partial"
			default:
				p.Status = "stopped"
			}
			sort.Slice(p.Services, func(i, j int) bool {
				return strings.ToLower(p.Services[i].Name) < strings.ToLower(p.Services[j].Name)
			})
			out = append(out, *p)
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Created == out[j].Created {
				return out[i].Name < out[j].Name
			}
			return out[i].Created > out[j].Created
		})
		return out, nil
	})
}

// ComposeAction 对整个编排项目批量操作：start | stop | restart | down（停并删容器，不动卷与网络）。
// 逐个服务执行、失败不中断，结果汇总成一句话回给前端；全部失败才回 error
func ComposeAction(project, action string) (string, error) {
	name := strings.TrimSpace(project)
	if name == "" {
		return "", fmt.Errorf("编排项目不能为空")
	}
	list, err := ListComposeProjects()
	if err != nil {
		return "", err
	}
	var target *ComposeProject
	for i := range list {
		if list[i].Name == name {
			target = &list[i]
			break
		}
	}
	if target == nil {
		return "", fmt.Errorf("没找到编排项目 %s（容器可能已被删除）", name)
	}

	verb := map[string]string{"start": "启动", "stop": "停止", "restart": "重启", "down": "移除"}[action]
	if verb == "" {
		return "", fmt.Errorf("不支持的编排操作：%s", action)
	}

	done, fails := 0, make([]string, 0, 2)
	for _, s := range target.Services {
		var e error
		switch action {
		case "start":
			if s.State == "running" {
				continue // 已经在跑的不用再起
			}
			e = StartContainer(s.ID)
		case "stop":
			if s.State != "running" {
				continue
			}
			e = StopContainer(s.ID, 10)
		case "restart":
			e = RestartContainer(s.ID) // 已停止的容器 restart 等价于 start
		case "down":
			e = RemoveContainer(s.ID, true)
		}
		if e != nil {
			fails = append(fails, s.Name+": "+e.Error())
			continue
		}
		done++
	}

	applog.Infof("docker", "编排 %s：%s完成 %d 个，失败 %d 个", name, verb, done, len(fails))
	if len(fails) > 0 {
		msg := fmt.Sprintf("%s完成 %d 个项目内服务，失败 %d 个：%s", verb, done, len(fails), strings.Join(fails, "；"))
		if done == 0 {
			return "", fmt.Errorf("%s", msg)
		}
		return msg, nil
	}
	if done == 0 {
		return fmt.Sprintf("没有需要%s的容器", verb), nil
	}
	return fmt.Sprintf("已%s %d 个容器", verb, done), nil
}
