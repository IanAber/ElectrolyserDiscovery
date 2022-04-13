package main

import (
	"fmt"
	"github.com/gorilla/mux"
	"github.com/simonvetter/modbus"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"
)

func closeConnection(conn net.Conn) {
	err := conn.Close()
	if err != nil {
		log.Println(err)
	}
}
func raw_connect(host string, port uint16) bool {
	timeout := time.Millisecond * 100
	sPort := fmt.Sprint(port)
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, sPort), timeout)
	if err != nil {
		return false
	}
	if conn != nil {
		defer closeConnection(conn)
		return true
	}
	return false
}

// Read and decode the serial number
func ReadSerialNumber(Client *modbus.ModbusClient) string {
	type Codes struct {
		Site    string
		Order   string
		Chassis uint32
		Day     uint8
		Month   uint8
		Year    uint16
		Product string
	}

	var codes Codes

	serialCode, err := Client.ReadUint64(14, modbus.INPUT_REGISTER)
	if err != nil {
		log.Println("Error getting serial number - ", err)
		return ""
	}

	//  1 bits - reserved, must be 0
	// 10 bits - Product Unicode
	// 11 bits - Year + Month
	//  5 bits - Day
	// 24 bits - Chassis Number
	//  5 bits - Order
	//  8 bits - Site

	Site := uint8(serialCode & 0xff)
	switch Site {
	case 0:
		codes.Site = "PI"
	case 1:
		codes.Site = "SA"
	default:
		codes.Site = "XX"
	}

	var Order [1]byte
	Order[0] = byte((serialCode>>8)&0x1f) + 64
	codes.Order = string(Order[:])

	codes.Chassis = uint32((serialCode >> 13) & 0xffffff)
	codes.Day = uint8((serialCode >> 37) & 0x1f)
	yearMonth := (serialCode >> 42) & 0x7ff
	codes.Year = uint16(yearMonth / 12)
	codes.Month = uint8(yearMonth % 12)
	Product := uint16((serialCode >> 53) & 0x3ff)

	var unicode [2]byte
	unicode[1] = byte(Product%32) + 64
	unicode[0] = byte(Product/32) + 64
	codes.Product = string(unicode[:])

	return fmt.Sprintf("%s%02d%02d%02d%02d%s%s", codes.Product, codes.Year, codes.Month, codes.Day, codes.Chassis, codes.Order, codes.Site)
}

func closeModbus(client *modbus.ModbusClient) {
	err := client.Close()
	if err != nil {
		log.Println(err)
	}
}

func testIP(ip string) (serial string, err error) {
	serial = ""
	if !raw_connect(ip, 502) {
		return "", fmt.Errorf("Nothing found at %s", ip)
	}
	var config modbus.ClientConfiguration
	config.Timeout = 100 * time.Millisecond // 1 second timeout
	config.URL = "tcp://" + ip + ":502"
	var Client *modbus.ModbusClient
	if Client, err = modbus.NewClient(&config); err != nil {
		fmt.Print("New modbus client error - ", err)
		return
	}
	if err := Client.Open(); err != nil {
		fmt.Print("Modbus client.open error - ", err)
	} else {
		defer closeModbus(Client)
		serial = ReadSerialNumber(Client)
		if serial == "" {
			err = fmt.Errorf("Failed to find the serial number for device at %s", ip)
		}
	}
	return
}

func getLocalIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Fatal(err)
	}
	defer closeConnection(conn)
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP
}

func SearchForElectrolysers(w http.ResponseWriter, r *http.Request) {
	bFound := false
	IPAddr := getLocalIP()

	if err := r.ParseForm(); err != nil {
		log.Println(err)
	}

	//	fmt.Fprintf(w, "Looking for electrolysers between %d and %d", r.FormValue("from"), r.FormValue("to"))
	from, err := strconv.ParseInt(r.FormValue("from"), 10, 16)
	if err != nil {
		if _, ferr := fmt.Fprintf(w, "Error - %v", err); ferr != nil {
			log.Println(ferr)
		}
		return
	}
	to, err := strconv.ParseInt(r.FormValue("to"), 10, 16)
	if err != nil {
		if _, ferr := fmt.Fprintf(w, "Error - %v", err); ferr != nil {
			log.Println(ferr)
		}
		return
	}
	if _, ferr := fmt.Fprintln(w, "<html><head><title>Results from Electrolyser Search</title></head><body><ul>"); ferr != nil {
		log.Println(ferr)
	}
	for IPAddr[3] = uint8(from); IPAddr[3] < uint8(to); IPAddr[3]++ {
		result, err := testIP(IPAddr.String())
		if err == nil {
			bFound = true
			if _, ferr := fmt.Fprintf(w, "<li>Found electrolyser at %s with serial number %s</li>", IPAddr.String(), result); ferr != nil {
				log.Println(ferr)
			}
		}
	}
	if !bFound {
		if _, ferr := fmt.Fprintf(w, "</ul><h1>No electrolysers were found between %d.%d.%d.%d and %d.%d.%d.%d.</h1>",
			IPAddr[0], IPAddr[1], IPAddr[2], from, IPAddr[0], IPAddr[1], IPAddr[2], to); ferr != nil {
			log.Println(ferr)
		}
	} else {
		if _, ferr := fmt.Fprint(w, "</ul>"); ferr != nil {
			log.Println(ferr)
		}
	}
	if _, ferr := fmt.Fprintln(w, "</body></html>"); ferr != nil {
		log.Println(ferr)
	}
}

func ShowHomePage(w http.ResponseWriter, _ *http.Request) {
	ip := getLocalIP()
	if _, ferr := fmt.Fprintf(w, `<html>
  <head>
    <title>Electrolyser Search</title>
  </head>
  <body>
    <div>
      <h1>Search for electrolysers in the local subnet %d.%d.%d.X</h1>
      <form action="/search" method="POST">
		<span style="font-size:x-large">
        Search Addresses from <input name="from" type="number" min="0" max="255" value="200" style="font-size:x-large" /> to <input name="to" type="number" min="0" max="255" value="254" style="font-size:x-large" /><br />
        <input style="font-size:x-large" type="submit" value="Search" />
		</span>
      </form>
    </div>
  </body>
</html>`, ip[0], ip[1], ip[2]); ferr != nil {
		log.Println(ferr)
	}
}

func main() {
	router := mux.NewRouter().StrictSlash(true)
	router.HandleFunc("/", ShowHomePage)
	router.HandleFunc("/search", SearchForElectrolysers).Methods("POST")
	log.Fatal(http.ListenAndServe(":8080", router))
}
