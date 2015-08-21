package main

import (
    "os/exec"
    "strings"
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

/*
        rest.Get("/countries", GetAllCountries),
        rest.Post("/countries", PostCountry),
        rest.Get("/countries/:code", GetCountry),
        rest.Delete("/countries/:code", DeleteCountry),
*/

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

    cmd := exec.Command("echo", "zfs", "clone", zpool + "/" + ami.Ami + "@0", zpool + "/" + u2)
    stdout, err := cmd.Output()
    if err != nil {
        panic(err)
    }
    print(string(stdout))

    // create network interface and bring up

    // create tap interface

    // TODO find new tap interface

    lock.Lock()
    cmd = exec.Command("echo", "ifconfig", "tap1", "create")
    stdout, err = cmd.Output()
    lock.Unlock()

    if err != nil {
        println(err.Error())
        return
    }

    print(string(stdout))

    // add tap interface to bridge 
    // TODO create separate bridge for each instance to separate instances

    lock.Lock()
    cmd = exec.Command("echo", "ifconfig", "bridge0", "addm", "tap1")
    stdout, err = cmd.Output()
    lock.Unlock()

    if err != nil {
        println(err.Error())
        return
    }

    print(string(stdout))

    lock.Lock()
    cmd = exec.Command("echo", "ifconfig", "bridge0", "up")
    stdout, err = cmd.Output()
    lock.Unlock()

    if err != nil {
        println(err.Error())
        return
    }

    print(string(stdout))

    // start the instance
    // # sh /usr/share/examples/bhyve/vmrun.sh -c 4 -m 1024M -t tap0 -d guest.img -i -I FreeBSD-10.0-RELEASE-amd64-bootonly.iso guestname
    // TODO destroy first: bhyvectl --destroy --vm bhyve01

    // /usr/sbin/bhyveload -c /dev/nmdm0A -m 512M -d /dev/zvol/boxy/i-7fa1e5db i-7fa1e5db

    lock.Lock()
    cmd = exec.Command("echo", "bhyveload", "-c", "/dev/nmdm0A", "-m", "512M", "-d", "/dev/zvol/" + zpool + "/" + u2, u2)
    stdout, err = cmd.Output()
    lock.Unlock()

    if err != nil {
        println(err.Error())
        return
    }

    print(string(stdout))

    // actually start VM
    // bhyve -c $cpu -A -H -P -m $ram $pci_args -lcom1,/dev/${con}A ioh-$name &
    // /usr/sbin/bhyve -c 1 -m 512M -H -A -P -g 0 -s 0:0,hostbridge -s 1:0,lpc -s 2:0,virtio-net,tap1 -s 3:0,virtio-blk,/dev/zvol/boxy/i-7fa1e5db -l com1,/dev/nmdm0A i-7fa1e5db

    lock.Lock()
    cmd = exec.Command("echo", "bhyve", "-c", "1", "-m", "512", "-H", "-A", "-P", "-s", "0:0,hostbridge", "-s", "1:0,lpc", "-s", "2:0,virtio-net,tap1", "-s", "3:0,virtio-blk,/dev/zvol/" + zpool + "/" + u2, "-lcom1,/dev/nmdm0A", u2)
    stdout, err = cmd.Output()
    lock.Unlock()

    if err != nil {
        println(err.Error())
        return
    }

    print(string(stdout))

    // record process id in property of instance dataset
    // # zfs set pangolin:pid=4321 boxy/ami-9cda20c6

    w.WriteJson(&u2)

}

func InstanceDestroy(w rest.ResponseWriter, r *rest.Request) {
}

// TODO make this not hard coded, allow uploading data for instance, etc
func ImageCreate(w rest.ResponseWriter, r *rest.Request) {
    u1 := uuid.NewV4()
    u2 := u1.String()
    u2 = "ami-" + u2[0:8]

    app := "echo"

    arg0 := "zfs"
    arg1 := "clone"
    arg2 := zpool + "/bhyve01@2015081817020001"
    arg3 := zpool + "/" + u2

    lock.Lock()
    cmd := exec.Command(app, arg0, arg1, arg2, arg3)
    stdout, err := cmd.Output()
    lock.Unlock()

    if err != nil {
        println(err.Error())
        return
    }

    print(string(stdout))

    w.WriteJson(&u2)
}


/*

func GetCountry(w rest.ResponseWriter, r *rest.Request) {
    code := r.PathParam("code")

    lock.RLock()
    var country *Country
    if store[code] != nil {
        country = &Country{}
        *country = *store[code]
    }
    lock.RUnlock()

    if country == nil {
        rest.NotFound(w, r)
        return
    }
    w.WriteJson(country)
}

func GetAllCountries(w rest.ResponseWriter, r *rest.Request) {
    lock.RLock()
    countries := make([]Country, len(store))
    i := 0
    for _, country := range store {
        countries[i] = *country
        i++
    }
    lock.RUnlock()
    w.WriteJson(&countries)
}


func PostCountry(w rest.ResponseWriter, r *rest.Request) {
    country := Country{}
    err := r.DecodeJsonPayload(&country)
    if err != nil {
        rest.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    if country.Code == "" {
        rest.Error(w, "country code required", 400)
        return
    }
    if country.Name == "" {
        rest.Error(w, "country name required", 400)
        return
    }
    lock.Lock()
    store[country.Code] = &country
    lock.Unlock()
    w.WriteJson(&country)
}

func DeleteCountry(w rest.ResponseWriter, r *rest.Request) {
    code := r.PathParam("code")
    lock.Lock()
    delete(store, code)
    lock.Unlock()
    w.WriteHeader(http.StatusOK)
}

*/
