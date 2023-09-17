package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/goombaio/namegenerator"
	"github.com/phayes/freeport"
	"github.com/sethvargo/go-password/password"
)

// Log error, print stdout and exit if error
func check(err error, stdout ...[]byte) {
	if err != nil {
		if len(stdout) > 0 {
			fmt.Println(string(stdout[0]))
		}
		log.Fatal(err)
	}
}

// Execute bash command and handle errors
func command(commands string, onFail ...func()) (stdout string) {
	cmd := exec.Command("bash", "-c", commands)
	stdo, err := cmd.Output()
	stdout = string(stdo)
	if err != nil {
		if len(onFail) > 0 {
			onFail[0]()
		}
		fmt.Println(stdout)
		log.Fatal(err)
	}
	return
}

// Create an lxc instance with following parameters using terraform
func create(name, image, terraFolder, filePath string, ports []int, ramLimit, cpuLimit, coreCount int) {
	var sshExtPort int

	// Create a .tf file describing a container
	file, err := os.Create(filePath)
	check(err)
	defer file.Close()

	writer := bufio.NewWriter(file)
	writer.WriteString(fmt.Sprintf(
		`resource "lxd_instance" "%v" {
	name = "%v"
	image = "images:%v"
	ephemeral = false
	
	config = {
		"boot.autostart" = true
		"security.nesting" = true
	}
	
	limits = {
		cpu = %v
		memory = "%vMB"
		"cpu.allowance" = "%v%%"
	}

	`, name, name, image, coreCount, ramLimit, cpuLimit))

	// Describe port forwarding rules
	for _, intPort := range ports {
		extPort, err := freeport.GetFreePort()
		check(err)
		fmt.Println(extPort)

		if intPort == 22 {
			sshExtPort = extPort
		}

		writer.WriteString(fmt.Sprintf(`
	device {
		name = "portForward%v"
		type = "proxy"
		properties = {
			listen = "tcp:0.0.0.0:%v"
			connect = "tcp:127.0.0.1:%v"
		}
	}
`, intPort, extPort, intPort))

		// Open ports assigned to the container
		// cmd := exec.Command("ufw", "allow", strconv.Itoa(intPort))
		// stdout, err := cmd.Output()
		// check(err, stdout)
	}
	writer.WriteString("}\n")
	writer.Flush()

	// Apply changes with terraform
	command(fmt.Sprintf("terraform -chdir=%v apply -auto-approve", terraFolder), func() { os.Remove(filePath) })

	// Retrieve an ip address of the container from lxc
	stdout := command(fmt.Sprintf("lxc list | grep %v", name))
	re := regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	matches := re.FindAllString(stdout, 1)
	if len(matches) == 0 {
		log.Fatal("Failed to get an ip from lxc")
	}
	ip := matches[0]

	// Choose setup script based on image
	var setupName string
	if strings.Contains(image, "ubuntu") || strings.Contains(image, "debian") {
		setupName = "deb_setup"
		// TODO other images
	} else {
		log.Fatal("No setup for provided image")
	}

	// Genrate 32 symbol passwd for root
	passwd, err := password.Generate(32, 10, 10, false, false)
	check(err)
	fmt.Println(passwd)

	// Run setup script which installs ssh server and sets root passwd
	// (It would be faster to copy existing template of a container, but terraform
	//  provider used by this script does not provide documentation on that)
	command(fmt.Sprintf("lxc file push /usr/share/contmgr/setup/%v.sh %v/root/", setupName, name))
	command(fmt.Sprintf("lxc exec %v -- sh /root/%v.sh '%v'", name, setupName, passwd))

	// Update known_hosts file
	command(fmt.Sprintf("ssh-keyscan -H %v >> $(eval echo ~$USER)/.ssh/known_hosts", ip))

	// Install public key to the container
	command(fmt.Sprintf("sshpass -p '%v' ssh-copy-id root@%v", passwd, ip))

	// Add ip to a list of ansible hosts
	command(fmt.Sprintf(`sed -i '/\[userContainers\]/a\%v' /etc/contmgr/ansible/hosts`, fmt.Sprintf("%v ansible_host=%v", name, ip)))

	fmt.Println(name)
	fmt.Printf("ssh root@von.sytes.net -p %v\n", sshExtPort)
}

func delete(terraFolder, name string) {
	err := os.Remove(path.Join(terraFolder, name+".tf"))
	if err != nil {
		log.Fatal("Failed to delete")
	}
	command(fmt.Sprintf("terraform -chdir=%v apply -auto-approve", terraFolder))
}

func listConts(terraFolder string) {
	entries, err := os.ReadDir(terraFolder)
	check(err)

	for _, e := range entries {
		if e.Name() == "main.tf" || e.Name() == ".tf" || !strings.HasSuffix(e.Name(), ".tf") {
			continue
		}
		fmt.Print(e.Name()[:len(e.Name())-3])

		// Print ip:port for ssh
		fmt.Printf("\tvon.sytes.net:%v\n", strings.Fields(
			command(`sed -n 's/.*0\.0\.0\.0:\([0-9]*\).*/\1/p' ` + path.Join(terraFolder, e.Name())))[0])
	}
}

func main() {
	var portsStr, name, image, terraFolder, filePath string
	var ports []int = []int{22}
	var ramLimit, cpuLimit, coreCount int
	var toDelete, toCreate, toList bool

	flag.StringVar(&name, "name", "", "Container name (default random)")
	flag.StringVar(&image, "image", "ubuntu/23.10", "Image")
	flag.StringVar(&portsStr, "ports", "", "Comma separated ports except 22 to forvard")
	flag.IntVar(&ramLimit, "ram", 100, "Limit of RAM (MB)")
	flag.IntVar(&cpuLimit, "cpu", 10, "Limit of CPU (%)")
	flag.IntVar(&coreCount, "core", 1, "Core number")
	flag.StringVar(&terraFolder, "path", "/etc/contmgr/terraform", "Path to directory with .tf files")
	flag.BoolVar(&toDelete, "d", false, "Delete specified container")
	flag.BoolVar(&toCreate, "c", false, "Create container")
	flag.BoolVar(&toList, "l", false, "List existing containers")

	flag.Parse()

	if fileInf, err := os.Stat(terraFolder); err != nil || !fileInf.IsDir() {
		log.Fatal("Invalid path")
	}

	if toList {
		listConts(terraFolder)
		return
	} else if toDelete && !toCreate {
		if name == "" {
			log.Fatal("Name is required")
		}
		filePath = path.Join(terraFolder, fmt.Sprintf("%v.tf", name))
		if _, err := os.Stat(filePath); errors.Is(err, os.ErrNotExist) {
			log.Fatal("Container not found")
		}
		delete(terraFolder, name)
		return
	} else if !toCreate {
		log.Fatal("Specify an action")
	}

	if name == "" {
		seed := time.Now().UTC().UnixNano()
		nameGenerator := namegenerator.NewNameGenerator(seed)
		name = nameGenerator.Generate()
	}

	filePath = path.Join(terraFolder, fmt.Sprintf("%v.tf", name))
	if _, err := os.Stat(filePath); !errors.Is(err, os.ErrNotExist) {
		log.Fatal("Name is already taken, choose another name")
	}

	if portsStr != "" {
		portsList := strings.Split(portsStr, ",")
		for _, port := range portsList {
			p, err := strconv.Atoi(port)
			check(err)
			if p < 0 || p > 65535 {
				log.Fatalf("Invalid port %v", p)
			}
			if slices.Contains(ports, p) {
				log.Fatal("Port specified twice")
			}
			ports = append(ports, p)
		}
	}

	if cpuLimit > 100 {
		log.Fatal("CPU limit cannot be more than 100%")
	}

	create(name, image, terraFolder, filePath, ports, ramLimit, cpuLimit, coreCount)
}
