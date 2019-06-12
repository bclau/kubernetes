/*
Copyright 2014 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package netexec

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	utilnet "k8s.io/apimachinery/pkg/util/net"
)

var (
	httpPort    = 8080
	udpPort     = 8081
	shellPath   = "/bin/sh"
	serverReady = &atomicBool{0}
)

// CmdNetexec is used by agnhost Cobra.
var CmdNetexec = &cobra.Command{
	Use:   "netexec",
	Short: "Creates a HTTP / UDP server with various endpoints",
	Long: `Starts a HTTP server on given TCP / UDP ports with the following endpoints:

- /: Returns the request's timestamp.
- /clientip: Returns the request's IP address.
- /dial: Creates a given number of requests to the given host and port using the given protocol,
  and returns a JSON with the fields "responses" (successful request responses) and "errors" (
  failed request responses). Returns "200 OK" status code if the last request succeeded,
  "417 Expectation Failed" if it did not, or "400 Bad Request" if any of the endpoint's parameters
  is invalid. The endpoint's parameters are:
  - "host": The host that will be dialed.
  - "port": The port that will be dialed.
  - "request": The HTTP endpoint or data to be sent through UDP. If not specified, it will result
    in a "400 Bad Request" status code being returned.
  - "protocol": The protocol which will be used when making the request. Default value: "http".
    Acceptable values: "http", "udp".
  - "tries": The number of times the request will be performed. Default value: "1".
- "/echo": Returns the given "msg" ("/echo?msg=echoed_msg")
- "/exit": Closes the server with the given code ("/exit?code=some-code"). The "code"
  is expected to be an integer [0-127] or empty; if it is not, it will return an error message.
- "/healthz": Returns "200 OK" if the server is ready, "412 Status Precondition Failed"
  otherwise. The server is considered not ready if the UDP server did not start yet or
  it exited.
- "/hostname": Returns the server's hostname.
- "/hostName": Returns the server's hostname.
- "/shell": Executes the given "shellCommand" or "cmd" ("/shell?cmd=some-command") and
  returns a JSON containing the fields "output" (command's output) and "error" (command's
  error message). Returns "200 OK" if the command succeeded, "417 Expectation Failed" if not.
- "/shutdown": Closes the server with the exit code 0.
- "/public/": Serve the files in the "/uploads" folder
- "/upload": Accepts a file to be uploaded, writing it in the "/uploads" folder on the host.
  Returns a JSON with the fields "output" (containing the file's name on the server) and
  "error" containing any potential server side errors.`,
	Args: cobra.MaximumNArgs(2),
	Run:  main,
}

func init() {
	CmdNetexec.Flags().IntVar(&httpPort, "http-port", 8080, "HTTP Listen Port")
	CmdNetexec.Flags().IntVar(&udpPort, "udp-port", 8081, "UDP Listen Port")
}

// atomicBool uses load/store operations on an int32 to simulate an atomic boolean.
type atomicBool struct {
	v int32
}

// set sets the int32 to the given boolean.
func (a *atomicBool) set(value bool) {
	if value {
		atomic.StoreInt32(&a.v, 1)
		return
	}
	atomic.StoreInt32(&a.v, 0)
}

// get returns true if the int32 == 1
func (a *atomicBool) get() bool {
	return atomic.LoadInt32(&a.v) == 1
}

func main(cmd *cobra.Command, args []string) {
	go startUDPServer(udpPort)
	startHTTPServer(httpPort)
}

func startHTTPServer(httpPort int) {
	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/clientip", clientIPHandler)
	http.HandleFunc("/echo", echoHandler)
	http.HandleFunc("/exit", exitHandler)
	http.HandleFunc("/hostname", hostnameHandler)
	http.HandleFunc("/shell", shellHandler)
	http.HandleFunc("/upload", uploadHandler)
	http.HandleFunc("/dial", dialHandler)
	http.HandleFunc("/healthz", healthzHandler)
	http.HandleFunc("/public/", publicHandler)
	// older handlers
	http.HandleFunc("/hostName", hostNameHandler)
	http.HandleFunc("/shutdown", shutdownHandler)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", httpPort), nil))
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("GET /")
	fmt.Fprintf(w, "NOW: %v", time.Now())
}

func echoHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("GET /echo?msg=%s", r.FormValue("msg"))
	fmt.Fprintf(w, "%s", r.FormValue("msg"))
}

func clientIPHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("GET /clientip")
	fmt.Fprintf(w, r.RemoteAddr)
}

func exitHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("GET /exit?code=%s", r.FormValue("code"))
	code, err := strconv.Atoi(r.FormValue("code"))
	if err == nil || r.FormValue("code") == "" {
		os.Exit(code)
	}
	fmt.Fprintf(w, "argument 'code' must be an integer [0-127] or empty, got %q", r.FormValue("code"))
}

func hostnameHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("GET /hostname")
	fmt.Fprintf(w, getHostName())
}

// healthHandler response with a 200 if the UDP server is ready. It also serves
// as a health check of the HTTP server by virtue of being a HTTP handler.
func healthzHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("GET /healthz")
	if serverReady.get() {
		w.WriteHeader(200)
		return
	}
	w.WriteHeader(http.StatusPreconditionFailed)
}

func publicHandler(w http.ResponseWriter, r *http.Request) {
	fs := http.StripPrefix("/public/", http.FileServer(http.Dir("/uploads")))
	w.Header().Set("Cache-Control", "private")
	// Needed for local proxy to Kubernetes API server to work.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "DNT,X-Mx-ReqToken,Keep-Alive,User-Agent,X-Requested-With,Cache-Control,Content-Type")
	// Disable If-Modified-Since so update-demo isn't broken by 304s
	r.Header.Del("If-Modified-Since")
	fs.ServeHTTP(w, r)
}

func shutdownHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("GET /shutdown")
	os.Exit(0)
}

func dialHandler(w http.ResponseWriter, r *http.Request) {
	values, err := url.Parse(r.URL.RequestURI())
	if err != nil {
		http.Error(w, fmt.Sprintf("%v", err), http.StatusBadRequest)
		return
	}

	host := values.Query().Get("host")
	port := values.Query().Get("port")
	request := values.Query().Get("request") // hostName
	protocol := values.Query().Get("protocol")
	tryParam := values.Query().Get("tries")
	log.Printf("GET /dial?host=%s&protocol=%s&port=%s&request=%s&tries=%s", host, protocol, port, request, tryParam)
	tries := 1
	if len(tryParam) > 0 {
		tries, err = strconv.Atoi(tryParam)
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("tries parameter is invalid. %v", err), http.StatusBadRequest)
		return
	}
	if len(request) == 0 {
		http.Error(w, fmt.Sprintf("request parameter not specified. %v", err), http.StatusBadRequest)
		return
	}
	if len(protocol) == 0 {
		protocol = "http"
	} else {
		protocol = strings.ToLower(protocol)
	}
	if protocol != "http" && protocol != "udp" {
		http.Error(w, fmt.Sprintf("unsupported protocol. %s", protocol), http.StatusBadRequest)
		return
	}

	hostPort := net.JoinHostPort(host, port)
	var udpAddress *net.UDPAddr
	if protocol == "udp" {
		udpAddress, err = net.ResolveUDPAddr("udp", hostPort)
		if err != nil {
			http.Error(w, fmt.Sprintf("host and/or port param are invalid. %v", err), http.StatusBadRequest)
			return
		}
	} else {
		_, err = net.ResolveTCPAddr("tcp", hostPort)
		if err != nil {
			http.Error(w, fmt.Sprintf("host and/or port param are invalid. %v", err), http.StatusBadRequest)
			return
		}
	}
	errors := make([]string, 0)
	responses := make([]string, 0)
	var response string
	for i := 0; i < tries; i++ {
		if protocol == "udp" {
			response, err = dialUDP(request, udpAddress)
		} else {
			response, err = dialHTTP(request, hostPort)
		}
		if err != nil {
			errors = append(errors, fmt.Sprintf("%v", err))
		} else {
			responses = append(responses, response)
		}
	}
	output := map[string][]string{}
	if len(response) > 0 {
		output["responses"] = responses
	}
	if len(errors) > 0 {
		output["errors"] = errors
	}
	bytes, err := json.Marshal(output)
	if err == nil {
		fmt.Fprintf(w, string(bytes))
	} else {
		http.Error(w, fmt.Sprintf("response could not be serialized. %v", err), http.StatusExpectationFailed)
	}
}

func dialHTTP(request, hostPort string) (string, error) {
	transport := utilnet.SetTransportDefaults(&http.Transport{})
	httpClient := createHTTPClient(transport)
	resp, err := httpClient.Get(fmt.Sprintf("http://%s/%s", hostPort, request))
	defer transport.CloseIdleConnections()
	if err == nil {
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err == nil {
			return string(body), nil
		}
	}
	return "", err
}

func createHTTPClient(transport *http.Transport) *http.Client {
	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}
	return client
}

func dialUDP(request string, remoteAddress *net.UDPAddr) (string, error) {
	Conn, err := net.DialUDP("udp", nil, remoteAddress)
	if err != nil {
		return "", fmt.Errorf("udp dial failed. err:%v", err)
	}

	defer Conn.Close()
	buf := []byte(request)
	_, err = Conn.Write(buf)
	if err != nil {
		return "", fmt.Errorf("udp connection write failed. err:%v", err)
	}
	udpResponse := make([]byte, 1024)
	Conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	count, err := Conn.Read(udpResponse)
	if err != nil || count == 0 {
		return "", fmt.Errorf("reading from udp connection failed. err:'%v'", err)
	}
	return string(udpResponse[0:count]), nil
}

func shellHandler(w http.ResponseWriter, r *http.Request) {
	cmd := r.FormValue("shellCommand")
	if cmd == "" {
		cmd = r.FormValue("cmd")
	}
	log.Printf("GET /shell?cmd=%s", cmd)
	cmdOut, err := exec.Command(shellPath, "-c", cmd).CombinedOutput()
	output := map[string]string{}
	if len(cmdOut) > 0 {
		output["output"] = string(cmdOut)
	}
	if err != nil {
		output["error"] = fmt.Sprintf("%v", err)
	}
	log.Printf("Output: %s", output)
	bytes, err := json.Marshal(output)
	if err == nil {
		fmt.Fprintf(w, string(bytes))
	} else {
		http.Error(w, fmt.Sprintf("response could not be serialized. %v", err), http.StatusExpectationFailed)
	}
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("GET /upload")
	result := map[string]string{}
	file, _, err := r.FormFile("file")
	if err != nil {
		result["error"] = "Unable to upload file."
		bytes, err := json.Marshal(result)
		if err == nil {
			fmt.Fprintf(w, string(bytes))
		} else {
			http.Error(w, fmt.Sprintf("%s. Also unable to serialize output. %v", result["error"], err), http.StatusInternalServerError)
		}
		log.Printf("Unable to upload file: %s", err)
		return
	}
	defer file.Close()

	f, err := ioutil.TempFile("/uploads", "upload")
	if err != nil {
		result["error"] = "Unable to open file for write"
		bytes, err := json.Marshal(result)
		if err == nil {
			fmt.Fprintf(w, string(bytes))
		} else {
			http.Error(w, fmt.Sprintf("%s. Also unable to serialize output. %v", result["error"], err), http.StatusInternalServerError)
		}
		log.Printf("Unable to open file for write: %s", err)
		return
	}
	defer f.Close()
	if _, err = io.Copy(f, file); err != nil {
		result["error"] = "Unable to write file."
		bytes, err := json.Marshal(result)
		if err == nil {
			fmt.Fprintf(w, string(bytes))
		} else {
			http.Error(w, fmt.Sprintf("%s. Also unable to serialize output. %v", result["error"], err), http.StatusInternalServerError)
		}
		log.Printf("Unable to write file: %s", err)
		return
	}

	UploadFile := f.Name()
	if err := os.Chmod(UploadFile, 0700); err != nil {
		result["error"] = "Unable to chmod file."
		bytes, err := json.Marshal(result)
		if err == nil {
			fmt.Fprintf(w, string(bytes))
		} else {
			http.Error(w, fmt.Sprintf("%s. Also unable to serialize output. %v", result["error"], err), http.StatusInternalServerError)
		}
		log.Printf("Unable to chmod file: %s", err)
		return
	}
	log.Printf("Wrote upload to %s", UploadFile)
	result["output"] = UploadFile
	w.WriteHeader(http.StatusCreated)
	bytes, err := json.Marshal(result)
	fmt.Fprintf(w, string(bytes))
}

func hostNameHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("GET /hostName")
	fmt.Fprintf(w, getHostName())
}

// udp server supports the hostName, echo and clientIP commands.
func startUDPServer(udpPort int) {
	serverAddress, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", udpPort))
	assertNoError(err)
	serverConn, err := net.ListenUDP("udp", serverAddress)
	assertNoError(err)
	defer serverConn.Close()
	buf := make([]byte, 1024)

	log.Printf("Started UDP server")
	// Start responding to readiness probes.
	serverReady.set(true)
	defer func() {
		log.Printf("UDP server exited")
		serverReady.set(false)
	}()
	for {
		n, clientAddress, err := serverConn.ReadFromUDP(buf)
		assertNoError(err)
		receivedText := strings.ToLower(strings.TrimSpace(string(buf[0:n])))
		if receivedText == "hostname" {
			log.Println("Sending udp hostName response")
			_, err = serverConn.WriteToUDP([]byte(getHostName()), clientAddress)
			assertNoError(err)
		} else if strings.HasPrefix(receivedText, "echo ") {
			parts := strings.SplitN(receivedText, " ", 2)
			resp := ""
			if len(parts) == 2 {
				resp = parts[1]
			}
			log.Printf("Echoing %v\n", resp)
			_, err = serverConn.WriteToUDP([]byte(resp), clientAddress)
			assertNoError(err)
		} else if receivedText == "clientip" {
			log.Printf("Sending back clientip to %s", clientAddress.String())
			_, err = serverConn.WriteToUDP([]byte(clientAddress.String()), clientAddress)
			assertNoError(err)
		} else if len(receivedText) > 0 {
			log.Printf("Unknown udp command received: %v\n", receivedText)
		}
	}
}

func getHostName() string {
	hostName, err := os.Hostname()
	assertNoError(err)
	return hostName
}

func assertNoError(err error) {
	if err != nil {
		log.Fatal("Error occurred. error:", err)
	}
}
