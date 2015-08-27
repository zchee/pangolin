package main

import (
	"github.com/ant0ine/go-json-rest/rest"
	. "github.com/mattn/go-getopt"
	"github.com/satori/go.uuid"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var zpool string
var listen string
var piddir string

func init() {
	var c int
	var pubinterface string
	// defaults
	listen = ":8080"
	piddir = "/var/run"

	OptErr = 0
	for {
		if c = Getopt("z:l:p:i:h"); c == EOF {
			break
		}
		switch c {
		case 'z':
			zpool = OptArg
		case 'l':
			listen = OptArg
		case 'p':
			piddir = OptArg
		case 'i':
			pubinterface = OptArg
		case 'h':
			usage()
			os.Exit(1)
		}
	}

	if zpool == "" {
		println("zpool required")
		usage()
		os.Exit(1)
	}

	if pubinterface == "" {
		println("public interface required")
		usage()
		os.Exit(1)
	}

	loadKmod("vmm")
	loadKmod("nmdm")
	loadKmod("if_bridge")
	loadKmod("if_tap")

	sysctlSet("net.link.tap.up_on_open", "1")
	bridgeCreate()
	bridgeAddPub(pubinterface)
}

func usage() {
	println("usage: pangolin [-z zpool|-l listenaddress|-p piddir|-i publicinterface|-h]")
}

func bridgeCreate() {
	lock.Lock()
	cmd := exec.Command("sudo", "ifconfig", "bridge0", "create")
	cmd.Output()
	cmd = exec.Command("sudo", "ifconfig", "bridge0", "up")
	cmd.Output()
	lock.Unlock()
}

func bridgeAddPub(publicinterface string) {
	lock.Lock()
	cmd := exec.Command("sudo", "ifconfig", "bridge0", "addm", publicinterface)
	cmd.Output()
	lock.Unlock()
}

func sysctlSet(sysctl string, value string) {
	lock.Lock()
	cmd := exec.Command("sudo", "/sbin/sysctl", sysctl + "=" + value)
	cmd.Output()
	lock.Unlock()
}

func loadKmod (module string) {
	lock.Lock()
	cmd := exec.Command("kldstat", "-m", module)
	_, err := cmd.Output()
	lock.Unlock()

	if err == nil {
		return
	}

	lock.Lock()
	cmd = exec.Command("sudo", "kldload", module)
	_, err = cmd.Output()
	lock.Unlock()

}

func main() {
	api := rest.NewApi()
	api.Use(rest.DefaultDevStack...)
	router, err := rest.MakeRouter(

		// TODO http://docs.aws.amazon.com/AWSEC2/latest/APIReference/OperationList-query.html

		rest.Post("/api/v1/images", ImageCreate),
		rest.Get("/api/v1/images", ImageList),

		rest.Post("/api/v1/instances", InstanceCreate),
		rest.Post("/api/v1/instances/:instanceid", InstanceStart),
		rest.Put("/api/v1/instances/:instanceid", InstanceStop),
		rest.Get("/api/v1/instances", InstanceList),
		rest.Delete("/api/v1/instances/:instanceid", InstanceDestroy),
	)
	if err != nil {
		log.Fatal(err)
	}
	api.SetApp(router)
	log.Fatal(http.ListenAndServe(listen, api.MakeHandler()))
}

type Instances struct {
	Instance string
	Running  bool
	Image    string
}

type Images struct {
	Imageid string
	Os      string
}

type Ima struct {
	Ima string
	Mem int
	Cpu int
}

var lock = sync.RWMutex{}

func ImageList(w rest.ResponseWriter, r *rest.Request) {
	lock.Lock()
	cmd := exec.Command("zfs", "list", "-H", "-t", "volume")
	stdout, err := cmd.Output()
	lock.Unlock()

	if err != nil {
		return
	}

	lines := strings.Split(string(stdout), "\n")
	imas := make([]Images, 0)

	for _, line := range lines {
		if strings.Contains(line, "ima-") {
			ima := Images{}
			n := strings.Split(line, "\t")[0]
			n = strings.Split(n, "/")[1]
			ima.Imageid = n
			ima.Os = getImaOs(ima.Imageid)
			imas = append(imas, ima)
		}
	}

	w.WriteJson(imas)
}

func getInstanceIma(instanceid string) string {
	lock.Lock()
	cmd := exec.Command("zfs", "get", "-H", "origin", zpool+"/"+instanceid)
	stdout, err := cmd.Output()
	lock.Unlock()

	if err != nil {
		return ""
	}
	if len(strings.Fields(string(stdout))) < 2 {
		return ""
	}
	origin := strings.Fields(string(stdout))[2]
	origin = strings.Split(origin, "/")[1]
	origin = strings.Split(origin, "@")[0]

	return origin
}

func getImaOs(imageid string) string {
	lock.Lock()
	cmd := exec.Command("sudo", "zfs", "get", "-H", "pangolin:os", zpool+"/"+imageid)
	stdout, err := cmd.Output()
	lock.Unlock()

	if err != nil {
		return ""
	}
	if len(strings.Fields(string(stdout))) < 2 {
		return ""
	}
	os := strings.Fields(string(stdout))[2]

	return os

}

func InstanceList(w rest.ResponseWriter, r *rest.Request) {
	lock.Lock()
	cmd := exec.Command("zfs", "list", "-H", "-t", "volume")
	stdout, err := cmd.Output()
	lock.Unlock()

	if err != nil {
		println(stdout)
		return
	}

	lines := strings.Split(string(stdout), "\n")

	instance_list := make([]Instances, 0)
	re, err := regexp.Compile(`^i-.*`)

	for _, line := range lines {
		if len(line) > 0 {
			i := strings.Split(line, "\t")[0]
			i = strings.Split(i, "/")[1]
			if re.MatchString(i) == true {
				inst := Instances{}
				inst.Instance = i
				_, err := getPid(inst.Instance)
				if err == nil {
					inst.Running = true
				}
				inst.Image = getInstanceIma(i)
				instance_list = append(instance_list, inst)
			}
		}
	}
	w.WriteJson(&instance_list)
}

func cloneIma(ima string, instanceid string) {
	cmd := exec.Command("sudo", "zfs", "clone", zpool+"/"+ima+"@0", zpool+"/"+instanceid)
	stdout, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	print(string(stdout))
}

func destroyClone(instanceid string) {
	cmd := exec.Command("sudo", "zfs", "destroy", zpool+"/"+instanceid)
	_, err := cmd.Output()

	if err != nil {
		return
	}
}

func setupTap(tap string) {
	lock.Lock()
	cmd := exec.Command("sudo", "ifconfig", tap, "destroy")
	stdout, err := cmd.Output()
	cmd = exec.Command("sudo", "ifconfig", tap, "create")
	stdout, err = cmd.Output()
	lock.Unlock()

	if err != nil {
		println("setupTap error: ")
		println(err.Error())
		return
	}

	print(string(stdout))
}

func addTapToBridge(tap string, bridge string) {
	lock.Lock()
	cmd := exec.Command("sudo", "ifconfig", bridge, "addm", tap)
	stdout, err := cmd.Output()
	lock.Unlock()

	if err != nil {
		println("addTapToBridge error: ")
		println(err.Error())
		return
	}

	print(string(stdout))
}

func bridgeUp(bridge string) {
	lock.Lock()
	cmd := exec.Command("sudo", "ifconfig", bridge, "up")
	stdout, err := cmd.Output()
	lock.Unlock()

	if err != nil {
		println(err.Error())
		return
	}

	print(string(stdout))
}

func bhyveLoad(console string, memory int, instanceid string) {
	cmd := exec.Command("sudo", "bhyveload", "-c", console, "-m", strconv.Itoa(memory)+"M", "-d", "/dev/zvol/"+zpool+"/"+instanceid, instanceid)
	stdout, err := cmd.CombinedOutput()

	if err != nil {
		println(err.Error())
		return
	}

	print(string(stdout))
}

func bhyveDestroy(instanceid string) {
	cmd := exec.Command("sudo", "bhyvectl", "--destroy", "--vm", instanceid)
	_, err := cmd.Output()

	if err != nil {
		return
	}
}

func execBhyve(console string, cpus int, memory int, tap string, instanceid string) {
	pidfile := piddir + "/pangolin." + instanceid + ".pid"
	cmd := exec.Command("sudo", "daemon", "-c", "-f", "-p", pidfile, "bhyve", "-c", strconv.Itoa(cpus), "-m", strconv.Itoa(memory), "-H", "-A", "-P", "-s", "0:0,hostbridge", "-s", "1:0,lpc", "-s", "2:0,virtio-net,"+tap, "-s", "3:0,virtio-blk,/dev/zvol/"+zpool+"/"+instanceid, "-lcom1,"+console, instanceid)
	stdout, err := cmd.CombinedOutput()

	if err != nil {
		println(err.Error())
		return
	}
	print(string(stdout))
}

func allocateTap() string {
	lock.Lock()
	cmd := exec.Command("ifconfig")
	stdout, err := cmd.Output()
	lock.Unlock()
	if err != nil {
		println(err.Error())
		return ""
	}

	lines := strings.Split(string(stdout), "\n")

	t := 0
	r, err := regexp.Compile("^tap" + strconv.Itoa(t) + ": .*")

	for _, line := range lines {
		if r.MatchString(line) == true {
			t = t + 1
			r, err = regexp.Compile("^tap" + strconv.Itoa(t) + ": .*")
		}
	}

	return "tap" + strconv.Itoa(t)
}

func freeTap(tap string) {
	// TODO check that name begins with "tap"
	cmd := exec.Command("sudo", "ifconfig", tap, "destroy")
	cmd.Output()
}

func findBridge() string {
	// TODO create separate bridge for each instance to separate instances
	return "bridge0"
}

func saveTap(tap string, instanceid string) {
	cmd := exec.Command("sudo", "zfs", "set", "pangolin:tap="+tap, zpool+"/"+instanceid)
	stdout, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	print(string(stdout))
}

func saveCpu(cpu int, instanceid string) {
	cmd := exec.Command("sudo", "zfs", "set", "pangolin:cpu="+strconv.Itoa(cpu), zpool+"/"+instanceid)
	stdout, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	print(string(stdout))
}

func saveMem(mem int, instanceid string) {
	cmd := exec.Command("sudo", "zfs", "set", "pangolin:mem="+strconv.Itoa(mem), zpool+"/"+instanceid)
	stdout, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	print(string(stdout))
}

func getTap(instanceid string) string {
	cmd := exec.Command("zfs", "get", "-H", "-s", "local", "pangolin:tap", zpool+"/"+instanceid)
	stdout, err := cmd.Output()
	if err != nil {
		return ""
	}
	if len(strings.Fields(string(stdout))) < 2 {
		return ""
	}
	tap := strings.Fields(string(stdout))[2]
	return tap
}

func getCpu(instanceid string) int {
	cmd := exec.Command("zfs", "get", "-H", "-s", "local", "pangolin:cpu", zpool+"/"+instanceid)
	stdout, err := cmd.Output()
	if err != nil {
		return -1
	}
	if len(strings.Fields(string(stdout))) < 2 {
		return -1
	}
	cpu, _ := strconv.Atoi(strings.Fields(string(stdout))[2])
	return cpu
}

func getMem(instanceid string) int {
	cmd := exec.Command("zfs", "get", "-H", "-s", "local", "pangolin:mem", zpool+"/"+instanceid)
	stdout, err := cmd.Output()
	if err != nil {
		return -1
	}
	if len(strings.Fields(string(stdout))) < 2 {
		return -1
	}
	mem, _ := strconv.Atoi(strings.Fields(string(stdout))[2])
	return mem
}

func getPid(instanceid string) (string, error) {
	pidfile := piddir + "/pangolin." + instanceid + ".pid"
	lock.Lock()
	cmd := exec.Command("sudo", "cat", pidfile)
	stdout, err := cmd.Output()
	lock.Unlock()
	if err != nil {
		return "", err
	}
	return string(stdout), nil
}

// takes an image id and creates a running instance from it
func InstanceCreate(w rest.ResponseWriter, r *rest.Request) {
	// get ima
	ima := Ima{}
	err := r.DecodeJsonPayload(&ima)
	if err != nil {
		rest.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if ima.Ima == "" {
		rest.Error(w, "ima required", 400)
		return
	}
	if ima.Mem == 0 {
		rest.Error(w, "memory required", 400)
		return
	}
	if ima.Cpu == 0 {
		rest.Error(w, "cpu required", 400)
		return
	}

	// start the instance
	os := getImaOs(ima.Ima)
	switch os {
	case "freebsd":
		// clone ima to instance
		instanceid := allocateInstanceId()
		cloneIma(ima.Ima, instanceid)

		// create network interface and bring up
		tap := allocateTap()
		if tap == "" {
			return
		}
		bridge := findBridge()
		setupTap(tap)
		addTapToBridge(tap, bridge)
		bridgeUp(bridge)

		// cleanup leftover instance if needed
		bhyveDestroy(instanceid)
		nmdm := "/dev/nmdm-" + instanceid + "-A"
		saveTap(tap, instanceid)
		saveCpu(ima.Cpu, instanceid)
		saveMem(ima.Mem, instanceid)
		go startFreeBSDVM(nmdm, ima.Cpu, ima.Mem, tap, instanceid)
		w.WriteJson(&instanceid)
	case "linux":
		// clone ima to instance
		instanceid := allocateInstanceId()
		cloneIma(ima.Ima, instanceid)

		// create network interface and bring up
		tap := allocateTap()
		if tap == "" {
			return
		}
		bridge := findBridge()
		setupTap(tap)
		addTapToBridge(tap, bridge)
		bridgeUp(bridge)

		//nmdm := "/dev/nmdm-" + instanceid + "-A"
		saveTap(tap, instanceid)
		saveCpu(ima.Cpu, instanceid)
		saveMem(ima.Mem, instanceid)
		// bhyveLoad(nmdm, ima.Mem, instanceid)
		// execBhyve(nmdm, ima.Cpu, ima.Mem, tap, instanceid)
		w.WriteJson(&instanceid)
	default:
		rest.Error(w, "unknown OS", 400)
	}
}

func startFreeBSDVM(console string, cpus int, memory int, tap string, instanceid string) {
	// cleanup leftover instance if needed
	bhyveDestroy(instanceid)
	bhyveLoad(console, memory, instanceid)
	execBhyve(console, cpus, memory, tap, instanceid)
}

func allocateInstanceId() string {
	u1 := uuid.NewV4()
	u2 := u1.String()
	u2 = "i-" + u2[0:8]
	return u2
}

func killInstance(instance string) {
	pid, _ := getPid(instance)
	if len(pid) > 0 {
		cmd := exec.Command("sudo", "kill", pid)
		cmd.Output()
	}

        var pidstate error
	pidstate = nil
	for pidstate == nil {
		cmd := exec.Command("sudo", "kill", "-0", pid)
		err := cmd.Start()
		if err != nil {
			log.Fatal(err)
		}
		pidstate = cmd.Wait()
		time.Sleep(500 * time.Millisecond)
	}

	bhyveDestroy(instance)
}

func InstanceStart(w rest.ResponseWriter, r *rest.Request) {
	instanceid := r.PathParam("instanceid")

	re, _ := regexp.Compile(`^i-.*`)
	if re.MatchString(instanceid) == false {
		return
	}

	_, err := getPid(instanceid)
	if err == nil {
		w.WriteJson(&instanceid)
		return
	}

	ima := getInstanceIma(instanceid)
	os := getImaOs(ima)

	switch os {
	case "freebsd":
		// create network interface and bring up
		tap := getTap(instanceid)
		if tap == "" {
			return
		}
		bridge := findBridge()
		setupTap(tap)
		addTapToBridge(tap, bridge)
		bridgeUp(bridge)

		// start the instance
		nmdm := "/dev/nmdm-" + instanceid + "-A"
		cpu := getCpu(instanceid)
		mem := getMem(instanceid)
		go startFreeBSDVM(nmdm, cpu, mem, tap, instanceid)
		w.WriteJson(&instanceid)
	default:
		rest.Error(w, "unknown OS", 400)
	}

}

func InstanceStop(w rest.ResponseWriter, r *rest.Request) {
	instance := r.PathParam("instanceid")

	re, _ := regexp.Compile(`^i-.*`)
	if re.MatchString(instance) == false {
		return
	}

	go killInstance(instance)
	return
}

func realInstanceDestroy(instance string) {
	killInstance(instance)

	tap := getTap(instance)
	if len(tap) > 0 {
		freeTap(tap)
	}

	// wait for VM to stop
	time.Sleep(1000 * time.Millisecond)

	destroyClone(instance)
}

func InstanceDestroy(w rest.ResponseWriter, r *rest.Request) {
	instance := r.PathParam("instanceid")

	re, _ := regexp.Compile(`^i-.*`)
	if re.MatchString(instance) == false {
		return
	}

	go realInstanceDestroy(instance)

	w.WriteJson(&instance)
}

func ImageCreate(w rest.ResponseWriter, r *rest.Request) {
	u1 := uuid.NewV4()
	u2 := u1.String()
	u2 = "ima-" + u2[0:8]

	lock.Lock()
	// TODO make this not hard coded, allow uploading data for instance, etc
	cmd := exec.Command("echo", "zfs", "clone", zpool+"/bhyve01@2015081817020001", zpool+"/"+u2)
	stdout, err := cmd.Output()
	lock.Unlock()

	if err != nil {
		println(err.Error())
		return
	}

	print(string(stdout))

	w.WriteJson(&u2)
}
