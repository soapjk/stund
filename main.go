package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"strings"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	yaml "gopkg.in/yaml.v2"

	"gortc.io/stun"
)

type Config struct {
	Net     string `yaml:"Net"`
	Address string `yaml:"Address"`
	Profile bool   `yaml:"Profile"`
}

var (
	network     = flag.String("net", "udp", "network to listen")
	address     = flag.String("addr", "0.0.0.0:3479", "address to listen")
	profile     = flag.Bool("profile", false, "profile")
	config_path = flag.String("config", "./config.yaml", "profile")
)

// Server is RFC 5389 basic server implementation.
//
// Current implementation is UDP only and not utilizes FINGERPRINT mechanism,
// nor ALTERNATE-SERVER, nor credentials mechanisms. It does not support
// backwards compatibility with RFC 3489.
type Server struct {
	Addr         string
	LogAllErrors bool
	log          Logger
}

// Logger is used for logging formatted messages.
type Logger interface {
	// Printf must have the same semantics as log.Printf.
	Printf(format string, args ...interface{})
}

var (
	defaultLogger     = logrus.New()
	software          = stun.NewSoftware("gortc/stund")
	errNotSTUNMessage = errors.New("not stun message")
)

func basicProcess(addr net.Addr, b []byte, req, res *stun.Message) error {
	if !stun.IsMessage(b) {
		return errNotSTUNMessage
	}
	if _, err := req.Write(b); err != nil {
		return errors.Wrap(err, "failed to read message")
	}
	var (
		ip   net.IP
		port int
	)
	switch a := addr.(type) {
	case *net.UDPAddr:
		ip = a.IP
		port = a.Port
	default:
		panic(fmt.Sprintf("unknown addr: %v", addr))
	}
	return res.Build(req,
		stun.BindingSuccess,
		software,
		&stun.XORMappedAddress{
			IP:   ip,
			Port: port,
		},
		stun.Fingerprint,
	)
}

func (s *Server) serveConn(c net.PacketConn, res, req *stun.Message) error {
	if c == nil {
		return nil
	}
	buf := make([]byte, 1024)
	n, addr, err := c.ReadFrom(buf)
	if err != nil {
		s.log.Printf("ReadFrom: %v", err)
		return nil
	}
	// s.log().Printf("read %d bytes from %s", n, addr)
	if _, err = req.Write(buf[:n]); err != nil {
		s.log.Printf("Write: %v", err)
		return err
	}
	if err = basicProcess(addr, buf[:n], req, res); err != nil {
		if err == errNotSTUNMessage {
			return nil
		}
		s.log.Printf("basicProcess: %v", err)
		return nil
	}
	_, err = c.WriteTo(res.Raw, addr)
	if err != nil {
		s.log.Printf("WriteTo: %v", err)
	}
	return err
}

// Serve reads packets from connections and responds to BINDING requests.
func (s *Server) Serve(c net.PacketConn) error {
	var (
		res = new(stun.Message)
		req = new(stun.Message)
	)
	for {
		if err := s.serveConn(c, res, req); err != nil {
			s.log.Printf("serve: %v", err)
			return err
		}
		res.Reset()
		req.Reset()
	}
}

// ListenUDPAndServe listens on laddr and process incoming packets.
func ListenUDPAndServe(serverNet, laddr string) error {
	c, err := net.ListenPacket(serverNet, laddr)
	if err != nil {
		return err
	}
	s := &Server{
		log: defaultLogger,
	}
	return s.Serve(c)
}

func normalize(address string) string {
	if len(address) == 0 {
		address = "0.0.0.0"
	}
	if !strings.Contains(address, ":") {
		address = fmt.Sprintf("%s:%d", address, stun.DefaultPort)
	}
	return address
}

func main() {
	flag.Parse()
	var _config Config
	yamlFile, err := ioutil.ReadFile(*config_path)
	if err != nil {
		log.Printf("read config file failed,path:%v,err:%v", *config_path, err)
		_config.Net = *network
		_config.Address = *address
		_config.Profile = *profile
	} else {
		err = yaml.Unmarshal(yamlFile, &_config)
		if err !=nil{
			log.Panicln("err:",err)
		}
		log.Printf("load config file:%+v",_config)
	}
	if _config.Profile {
		go func() {
			log.Println(http.ListenAndServe("localhost:6060", nil))
		}()
	}
	switch _config.Net {
	case "udp":
		normalized := normalize(_config.Address)
		fmt.Println("gortc/stund listening on", normalized, "via", _config.Net)
		log.Fatal(ListenUDPAndServe(_config.Net, normalized))
	default:
		log.Fatalln("unsupported network:", _config.Net)
	}
}
