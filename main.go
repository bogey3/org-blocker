package main

import (
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/openrdap/rdap"
	"io/ioutil"
	"math/bits"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type OrgCache struct{
	cache map[string]struct{
		organization string
		blocked bool
	}
	mutex sync.Mutex
}

func(o *OrgCache)get(ip string)(bool, string, bool){
	val, exists := o.cache[ip]
	return exists, val.organization, val.blocked
}

func(o * OrgCache)set(ip string, organization string, block bool){
	o.mutex.Lock()
	defer o.mutex.Unlock()
	if _, exists := o.cache[ip]; !exists{
		o.cache[ip] = struct{organization string; blocked bool}{organization: organization, blocked: block}
	}
}

func ip2int(ip net.IP) uint32 {
	if len(ip) == 16 {
		return binary.BigEndian.Uint32(ip[12:16])
	}
	return binary.BigEndian.Uint32(ip)
}

func testIP(host *net.TCPAddr)(bool, string) {
	fmt.Println("-> Finding organization")
	//data, err := rdapClient.QueryIP(host.IP.String())
	data, err := rdapClient.QueryIP("109.127.2.69")
	var network *net.IPNet
	if err == nil {
		if data.IPVersion == "v4" {
			startAddress := net.ParseIP(data.StartAddress)
			endAddress := net.ParseIP(data.EndAddress)
			subnetMask := bits.OnesCount32(ip2int(endAddress) &^ ip2int(startAddress))
			_, network, err = net.ParseCIDR(fmt.Sprintf("%s/%d", data.StartAddress, subnetMask))
			if err != nil {
				_, network, err = net.ParseCIDR(fmt.Sprintf("%s/%s", data.StartAddress, startAddress.DefaultMask().String()))
			}
		}

		if exists, org, blocked := orgCache.get(network.String()); exists {
			fmt.Println("-> Organization cache hit")
			return blocked, org
		}

		formatedNames := []string{}
		if data.Entities != nil {
			for _, entity := range data.Entities {
				if entity.VCard != nil && entity.VCard.Properties != nil{
					for _, property := range entity.VCard.Properties {
						if property.Name == "fn" {
							formatedNames = append(formatedNames, property.Value.(string))
							for _, org := range config.BlockedOrganizations {
								if strings.Contains(property.Value.(string), org) {
									go orgCache.set(network.String(), org, true)
									return true, org
								}
							}
						}
					}
				}
			}
		}
		if network == nil {
			go orgCache.set(host.IP.String(), strings.Join(formatedNames, ", "), false)
		} else {
			go orgCache.set(network.String(), strings.Join(formatedNames, ", "), false)
		}
		return false, strings.Join(formatedNames, ", ")
	}else {
		if network == nil {
			go orgCache.set(host.IP.String(), "Unknown organization", false)
		} else {
			go orgCache.set(network.String(), "Unknown organization", false)
		}
		return false, "Unknown organization"
	}
}

func readConfig()Config{
	out := Config{}
	if data, err := ioutil.ReadFile("config.json"); err == nil{
		json.Unmarshal(data, &out)
	}else{
		fmt.Println("Could not read config.json\n",err)
		os.Exit(0)
	}
	return out
}

func handleConnection(client net.Conn, blocked bool) {
	if !blocked {
		client.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n<h1>It Worked!</h1>"))
	}else{
		client.Write([]byte("HTTP/1.1 302 OK\r\nContent-Type: text/html; charset=UTF-8\r\nLocation: https://www.youtube.com/watch?v=dQw4w9WgXcQ\r\n\r\n"))
	}
	time.Sleep(time.Millisecond * 10)
	client.Close()
}


func handleClient(client net.Conn) {

	host, _ := client.RemoteAddr().(*net.TCPAddr)
	if len(config.BlockedOrganizations) > 0 {
		start := time.Now()
		blockedOrg, org := testIP(host)
		fmt.Println("-> Organization lookup took", time.Since(start))
		if blockedOrg {
			fmt.Println("-> Organization", `"`+org+`"`, "has been blocked\n")
		} else {
			fmt.Println("->", org, "not in block list\n")
		}
		handleConnection(client, blockedOrg)
	}
}

var rdapClient *rdap.Client
var orgCache OrgCache
var config Config

func main() {
	orgCache.cache = make(map[string]struct{organization string; blocked bool})
	rdapClient = &rdap.Client{}
	config = readConfig()

	if config.ListenSSL{
		if cert, err := tls.LoadX509KeyPair(config.Certificate, config.Key); err == nil{
			tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}
			if server, err := tls.Listen("tcp", config.ListenHost + ":" + strconv.Itoa(config.ListenPort), tlsConfig); err == nil{
				defer server.Close()
				fmt.Println("Started TCP server on", server.Addr())
				for{
					if conn, err := server.Accept(); err == nil {
						fmt.Println("-> Connection from", conn.RemoteAddr())
						go handleClient(conn)
					}else{
						fmt.Println(err)
					}
				}
			}else{
				panic(err)
			}
		}else{
			panic(err)
		}

	}

	if server, err := net.Listen("tcp", config.ListenHost + ":" + strconv.Itoa(config.ListenPort)); err == nil {
		fmt.Println("Started TCP server on", server.Addr())
		for {
			if conn, err := server.Accept(); err == nil {
				fmt.Println("-> Connection from", conn.RemoteAddr())
				go handleClient(conn)
			}else{
				fmt.Println(err)
			}
		}
	}
}

type Config struct {
	ListenHost           string   `json:"listenHost"`
	ListenPort           int      `json:"listenPort"`
	ListenSSL            bool     `json:"listenSSL"`
	Certificate          string   `json:"certificate"`
	Key                  string   `json:"key"`
	BlockedOrganizations []string `json:"blockedOrganizations"`
}
