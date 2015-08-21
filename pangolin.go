package main

import (
    "os/exec"
    "strings"
    "strconv"
//    "regexp"
//    "fmt"
    "github.com/ant0ine/go-json-rest/rest"
//    "github.com/mistifyio/go-zfs"
    "github.com/satori/go.uuid"
    "log"
    "net/http"
    "sync"
)

// TODO read config file
var zpool = "boxy"

func main() {
    api := rest.NewApi()
    api.Use(rest.DefaultDevStack...)
    router, err := rest.MakeRouter(

        // TODO http://docs.aws.amazon.com/AWSEC2/latest/APIReference/OperationList-query.html

        rest.Post("/api/images", ImageCreate),
        rest.Get("/api/images", ImageList),

        rest.Post("/api/instances", InstanceStart),
        rest.Get("/api/instances", InstanceList),
        rest.Delete("/api/instances", InstanceDestroy),

    )
    if err != nil {
        log.Fatal(err)
    }
    api.SetApp(router)
    log.Fatal(http.ListenAndServe(":8080", api.MakeHandler()))
}

type Country struct {
    Code string
    Name string
}

type Instances struct {
    Ami string
    Pid string
}

type Ami struct {
    Ami string
}

var store = map[string]*Country{}

var lock = sync.RWMutex{}

func ImageList(w rest.ResponseWriter, r *rest.Request) {

    lock.Lock()
    cmd := exec.Command("zfs", "list", "-H", "-t", "volume")
    stdout, err := cmd.Output()
    lock.Unlock()

    if err != nil {
        println(err.Error())
        return
    }

    lines := strings.Split(string(stdout), "\n")
    amis := make([]string, 0)

    for _, line := range lines {
       if strings.Contains(line, "ami-") {
           n := strings.Split(line, "\t")[0]
           n = strings.Split(n, "/")[1]
           amis = append(amis, n)
       }
    }

    w.WriteJson(amis)
}

func InstanceList(w rest.ResponseWriter, r *rest.Request) {
    lock.Lock()
    cmd := exec.Command("zfs", "get", "-H", "-s", "local", "-t", "volume", "pangolin:pid")
    stdout, err := cmd.Output()
    lock.Unlock()

    if err != nil {
        println(err.Error())
        return
    }

    lines := strings.Split(string(stdout), "\n")
    instance_list := make([]Instances,0)

    for _, line := range lines {
       if strings.Contains(line, "ami-") {
           ami := strings.Split(line, "\t")[0]
           ami = strings.Split(ami, "/")[1]
           pid := strings.Split(line, "\t")[2]
           inst := Instances{}
           inst.Pid = pid
           inst.Ami = ami
           instance_list = append(instance_list, inst)
       }
    }

    w.WriteJson(instance_list)

}

func cloneAmi(ami string, instanceid string) {
    cmd := exec.Command("echo", "zfs", "clone", zpool + "/" + ami + "@0", zpool + "/" + instanceid)
    stdout, err := cmd.Output()
    if err != nil {
        panic(err)
    }
    print(string(stdout))
}

func setupTap(tap string) {
    lock.Lock()
    cmd := exec.Command("echo", "ifconfig", tap, "create")
    stdout, err := cmd.Output()
    lock.Unlock()

    if err != nil {
        println(err.Error())
        return
    }

    print(string(stdout))
}

func addTapToBridge(tap string, bridge string) {
    lock.Lock()
    cmd := exec.Command("echo", "ifconfig", bridge, "addm", tap)
    stdout, err := cmd.Output()
    lock.Unlock()

    if err != nil {
        println(err.Error())
        return
    }

    print(string(stdout))
}

func bridgeUp(bridge string) {
    lock.Lock()
    cmd := exec.Command("echo", "ifconfig", bridge, "up")
    stdout, err := cmd.Output()
    lock.Unlock()

    if err != nil {
        println(err.Error())
        return
    }

    print(string(stdout))
}

func bhyveLoad(console string, memory int, instanceid string) {
    lock.Lock()
    cmd := exec.Command("echo", "bhyveload", "-c", console, "-m", strconv.Itoa(memory) + "M", "-d", "/dev/zvol/" + zpool + "/" + instanceid, instanceid)
    stdout, err := cmd.Output()
    lock.Unlock()

    if err != nil {
        println(err.Error())
        return
    }

    print(string(stdout))
}

func bhyveDestroy (instanceid string) {
    lock.Lock()
    cmd := exec.Command("echo", "bhyvectl", "--destroy", "--vm", instanceid)
    stdout, err := cmd.Output()
    lock.Unlock()

    if err != nil {
        println(err.Error())
        return
    }
    print(string(stdout))
}

func execBhyve(console string, cpus int, memory int, tap string, instanceid string) {
    pidfile := "/var/tmp/pangolin." + instanceid + ".pid"
    lock.Lock()
    cmd := exec.Command("echo", "daemon", "-c", "-f", "-p", pidfile, "bhyve", "-c", strconv.Itoa(cpus), "-m", strconv.Itoa(memory), "-H", "-A", "-P", "-s", "0:0,hostbridge", "-s", "1:0,lpc", "-s", "2:0,virtio-net," + tap, "-s", "3:0,virtio-blk,/dev/zvol/" + zpool + "/" + instanceid, "-lcom1," + console, instanceid)
    stdout, err := cmd.Output()
    lock.Unlock()

    if err != nil {
        println(err.Error())
        return
    }
    print(string(stdout))
}

func findTap() string {
    // TODO find new tap interface
    // TODO bring tap up? net.link.tap.up_on_open=1 should ensure it, but
    // paranoia, perhaps should have setupTap check state of
    // net.link.tap.up_on_open or do it at startup after reading config
    return "tap1"
}

func findBridge() string {
    // TODO create separate bridge for each instance to separate instances
    return "bridge0"
}

// takes an image id and creates a running instance from it
func InstanceStart(w rest.ResponseWriter, r *rest.Request) {
    // get ami
    ami := Ami{}
    err := r.DecodeJsonPayload(&ami)
    if err != nil {
        rest.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    if ami.Ami == "" {
        rest.Error(w, "ami required", 400)
        return
    }

    // clone ami to instance
    u1 := uuid.NewV4()
    u2 := u1.String()
    u2 = "i-" + u2[0:8]

    cloneAmi(ami.Ami, u2)

    // create network interface and bring up
    tap := findTap()
    bridge := findBridge()
    setupTap(tap)
    addTapToBridge(tap, bridge)
    bridgeUp(bridge)

    // start the instance
    bhyveDestroy(u2)
    bhyveLoad("/dev/nmdm0A", 512, u2)
    execBhyve("/dev/nmdm0A", 1, 512, tap, u2)
    w.WriteJson(&u2)
}

func InstanceDestroy(w rest.ResponseWriter, r *rest.Request) {
}

// TODO make this not hard coded, allow uploading data for instance, etc
func ImageCreate(w rest.ResponseWriter, r *rest.Request) {
    u1 := uuid.NewV4()
    u2 := u1.String()
    u2 = "ami-" + u2[0:8]

    lock.Lock()
    cmd := exec.Command("echo", "zfs", "clone", zpool + "/bhyve01@2015081817020001", zpool + "/" + u2)
    stdout, err := cmd.Output()
    lock.Unlock()

    if err != nil {
        println(err.Error())
        return
    }

    print(string(stdout))

    w.WriteJson(&u2)
}
