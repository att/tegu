// vi: sw=4 ts=4:

/*
	Mnemonic:	http_mirror_api
	Abstract:	This provides the API interface for mirroring (all URLs underneath /tegu/mirrors/).
				The main work functions (parse_get, parse_post, parse_delete) all generate
				json formatted data to the output device (we assume back to the requesting
				browser/user-agent).  The output should be an array (reqstate) with one "object" describing
				the result of each request, and a final object (endstate) describing the overall state.

				These requests are supported:
					POST /tegu/mirrors/
					DELETE /tegu/mirrors/<name>/[?cookie=cookie]
					GET /tegu/mirrors/
					GET /tegu/mirrors/<name>/[?cookie=cookie]

	Author:		Robert Eby

	Mods:		17 Feb 2015 - Created.
				20 Mar 2014 - Added support for specifying ports via MACs
				27 Apr 2015 - allow IPv6 for <output> GRE address, fixed bug with using label for output spec
*/

package managers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"codecloud.web.att.com/gopkgs/ipc"
	"codecloud.web.att.com/tegu/gizmos"
)

// globals used by generateMirrorName()
var mutex *sync.Mutex = &sync.Mutex{}
var lastid int64

func generateMirrorName( ) ( string ) {
	mutex.Lock()
	defer mutex.Unlock()
	now := time.Now().Unix()
	if now <= lastid {
		now = lastid+1
	}
	lastid = now
	return fmt.Sprintf( "mir-%x", now )
}

/*
 *	Convert "s" to startt, and "e" to endt.
 *	If s is "", set startt to "now"
 *	If e is "+nnn", set endt to start + nnn.
 *	If e is "+unbounded", set endt to 1/1/3000.
 */
func checkTimes(s string, e string) (startt int64, endt int64, err error) {
	err = nil
	now := time.Now().Unix()

	if s == "" {
		startt = now
	} else {
		startt, err = strconv.ParseInt(s, 0, 64)
	}
	if e[0:1] == "+" {
		endt, err = strconv.ParseInt(e[1:], 0, 64)
		endt += startt
	} else if e == "unbounded" {
		endt = 32503680000		// 1/1/3000
	} else {
		endt, err = strconv.ParseInt(e, 0, 64)
	}
	if err == nil {
		if startt < now {
			startt = now
		}
		if endt <= startt {
			err = fmt.Errorf( "end_time (%d) <= start_time, (%d)", endt, startt )
		} else {
			t := cfg_data["mirroring"]["min_mirror_expiration"]
			if t != nil {
				min, _ := strconv.ParseInt(*t, 0, 64)
				if (endt - startt) < min {
					err = fmt.Errorf( "end_time - start_time (%d) is less than the minimum interval (%d)", (endt - startt), min )
				}
			}
		}
	}
	return
}

/*
 * Validates a comma-separated list of VLAN IDs.  Valid if no error.
 */
func validVlanList(v string) (err error) {
	ss := strings.Split(v, ",")
	for _, v := range ss {
		n, _ := strconv.ParseInt(v, 10, 32)
		if n < 0 || n > 4095 {
			err = fmt.Errorf( "VLAN id is out of range: %s", v )
			return
		}
	}
	return
}

func validName(v string) (bool) {
	re := regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)
	return re.MatchString(v)
}

/*
 * Get the name of a mirror, and the "cookie" CGI argument (if any) from the HTTP request,
 * which is expected to look like this: /tegu/mirrors/<name>/?cookie=<cookie>
 * Note: this is not an HTTP cookie (unfortunate choice of name).
 */
func getNameAndCookie(in *http.Request) (name string, cookie string) {
	t := in.URL.Path
	tt := strings.Split(t, "/")
	if len(tt) == 5 {
		name = tt[3]
	} else {
		name = ""
	}
	v := in.URL.Query()
	cookie = v.Get("cookie")
	return
}

/*
 * Given a name, find the mirror that goes with the name.
 */
func lookupMirror(name string, cookie string) (mirror *gizmos.Pledge) {
	req := ipc.Mk_chmsg( )
	my_ch := make( chan *ipc.Chmsg )					// allocate channel for responses to our requests
	defer close( my_ch )
	req.Send_req( rmgr_ch, my_ch, REQ_GET, [] *string { &name, &cookie }, nil )
	req = <- my_ch
	if req.State == nil {
		mirror = req.Response_data.( *gizmos.Pledge )
	}
	return
}

/*
 * Return a string array of mirror names in the reservation cache.
 */
func getMirrors() ([]string) {
	req := ipc.Mk_chmsg( )
	my_ch := make( chan *ipc.Chmsg )							// allocate channel for responses to our requests
	defer close( my_ch )
	req.Send_req( rmgr_ch, my_ch, REQ_GET_MIRRORS, nil, nil )	// push it into the reservation manager which will drive flow-mods etc
	req = <- my_ch
	if req.State == nil {
		rv := string( *req.Response_data.(*string) )
		return strings.Split(rv, " ")
	} else {
		return []string { }
	}
}

func safe(s *string) ( string) {
	if s == nil {
		return "NIL"
	}
	return *s
}
/*
 * Convert a pledge into the JSON form needed by the API, which is not the same as the JSON in pledge.go
 * since that reflects the underlying pledge structure.
 */
func convertToJSON(mirror *gizmos.Pledge, scheme string, host string) (string) {
	// Arrgh!
	ports, outp, _, _, start, end, _, _ := mirror.Get_values()

	bs := bytes.NewBufferString("{\n")
	bs.WriteString(fmt.Sprintf("  \"name\": \"%s\",\n", *mirror.Get_id()))
	bs.WriteString(fmt.Sprintf("  \"start_time\": %d,\n", start))
	bs.WriteString(fmt.Sprintf("  \"end_time\": %d,\n", end))
//	if mirror.usrkey != "" {
//		// No harm including this since the user needed to provide it anyway
//		bs.WriteString(fmt.Sprintf(`  "cookie": "%s",\n`, mirror.usrkey))
//	}
	bs.WriteString(fmt.Sprintf("  \"port\": [\n    "))
	sep := ""
	vlan := ""
	for _, v := range strings.Split(*ports, " ") {
		if strings.HasPrefix(v, "vlan:") {
			vlan = v[5:]
		} else {
			bs.WriteString(fmt.Sprintf(`%s"%s"`, sep, v))
			sep = ", "
		}
	}
	bs.WriteString(fmt.Sprintf("\n  ],\n"))
	bs.WriteString(fmt.Sprintf("  \"output\": \"%s\",\n", *outp))
	if vlan != "" {
		bs.WriteString(fmt.Sprintf("  \"vlan\": \"%s\",\n", vlan))
	}
	// Other, informational (non-API) fields
	bs.WriteString(fmt.Sprintf("  \"physical_host\": \"%s\",\n", *mirror.Get_qid()))
	bs.WriteString(fmt.Sprintf("  \"pushed\": %t,\n",   mirror.Is_pushed()))
	bs.WriteString(fmt.Sprintf("  \"paused\": %t,\n",   mirror.Is_paused()))
	bs.WriteString(fmt.Sprintf("  \"pending\": %t,\n",  mirror.Is_pending()))
	bs.WriteString(fmt.Sprintf("  \"active\": %t,\n",   mirror.Is_active()))
	bs.WriteString(fmt.Sprintf("  \"expired\": %t,\n",  mirror.Is_extinct(0)))
	bs.WriteString(fmt.Sprintf("  \"url\": \"%s://%s/tegu/mirrors/%s/\"\n", scheme, host, *mirror.Get_id()))
	bs.WriteString("}\n")
	return bs.String()
}

type MirrorInfo struct {
	name		string		// mirror name
	physhost	string		// phys host for this mirror
	ports		[]string	// ports in this mirror
	err			error		// error, if this mirror cannot be created
}

/*
 * Validates the array of ports passed in.  Returns an array of MirrorInfo, one
 * per physical host, with a mirror name assigned, and a list of ports on that host.
 */
func validatePorts(ports []string, name string) (plist *map[string]MirrorInfo, err error) {
	valid := false
	namemap := make( map[string]MirrorInfo )
	plist = &namemap
	ix := 0
	badports := *new( []string )
	for _, p := range ports {
		vm, err := validatePort(&p)		// vm is a Net_vm
		if err == nil {
			if vm.phost != nil {
				// get info for port set physhost
				phys := *vm.phost

				nm := namemap[phys]
				if nm.name == "" {
					s := fmt.Sprintf("%s_%d", name, ix)
					ix++
					namemap[phys] = MirrorInfo {
						name: s,
						physhost: phys,
						ports: [] string { *vm.mac },
					}
				} else {
					// append port - Go can be SO annoying!
					namemap[phys] = MirrorInfo {
						name:     nm.name,
						physhost: nm.physhost,
						ports:    append(nm.ports, *vm.mac),
					}
				}
				valid = true
			} else {
				// invalid port: add to a group by itself, and report back
				http_sheep.Baa( 1, " invalid port? port = " + p )
				badports = append(badports, p)
			}
		} else {
			// invalid port: add to a group by itself, and report back
			http_sheep.Baa( 1, " invalid port? " + err.Error() )
			badports = append(badports, p)
		}
	}
	if !valid {
		err = fmt.Errorf("No valid ports found.")
	}
	if len(badports) > 0 {
		mi := new (MirrorInfo)
		mi.name  = "_badports_"
		mi.ports = badports
		mi.err   = fmt.Errorf("Invalid ports found.")
		namemap["_badports_"] = *mi
	}
	if ix == 1 {
		// Only 1 name used, use the unindexed name
		for _, nm := range namemap {
			if nm.err == nil {
				nm.name = name
			}
		}
	}
	return
}

/*
 * Validate a port.
 */
func validatePort(port *string) (vm *Net_vm, err error) {
	// handle mac:port form
	if strings.HasPrefix(*port, "mac:") {
		// Map the port MAC to a phost
		mac := (*port)[4:]

		my_ch := make( chan *ipc.Chmsg )
		defer close ( my_ch )

		req := ipc.Mk_chmsg( )
		req.Send_req( nw_ch, my_ch, REQ_GET_PHOST_FROM_MAC, &mac, nil )			// request MAC -> phost translation
		req = <- my_ch
		if req.Response_data == nil {
			err = fmt.Errorf("Cannot find MAC: " + mac)
		} else {
			vm = Mk_netreq_vm( nil, nil, nil, nil, req.Response_data.(*string), &mac, nil, nil, nil )	// only use the two fields
			http_sheep.Baa( 1, "name=NIL id=NIL ip4=NIL phost=%s mac=%s gw=NIL fip=NIL", safe(vm.phost), safe(vm.mac) )
		}
		return
	}

	// handle project/host form
	my_ch := make( chan *ipc.Chmsg )							// allocate channel for responses to our requests
	defer close ( my_ch )

	req := ipc.Mk_chmsg( )
	req.Send_req( osif_ch, my_ch, REQ_GET_HOSTINFO, port, nil )				// request data
	req = <- my_ch
	if req.Response_data != nil {
		vm = req.Response_data.( *Net_vm )
		if vm.phost == nil {
			// There seems to be a bug in REQ_GET_HOSTINFO, such that the 2nd call works
			req.Send_req( osif_ch, my_ch, REQ_GET_HOSTINFO, port, nil )
			req = <- my_ch
			vm = req.Response_data.( *Net_vm )
		}
		http_sheep.Baa( 1, "name=%s id=%s ip4=%s phost=%s mac=%s gw=%s fip=%s", safe(vm.name), safe(vm.id), safe(vm.ip4), safe(vm.phost), safe(vm.mac), safe(vm.gw), safe(vm.fip) )
	} else {
		if req.State != nil {
			err = req.State
		}
	}
	return
}

func cidrMatches(ip net.IP, cidr string) (bool) {
	_, net, err := net.ParseCIDR(cidr)
	if err != nil {
		http_sheep.Baa( 1, "Invalid CIDR for allowed_gre_addr in the configuration file: %s", cidr )
		return false
	}
	return net.Contains(ip)
}

func validateAllowedOutputIP(port *string) (err error) {
	oklist := cfg_data["mirroring"]["allowed_gre_addr"]
	if oklist != nil {
		ip := net.ParseIP(*port)
		if ip == nil {
			err = fmt.Errorf("output GRE port %s is not a valid IP address.", *port)
			return
		}
		for _, cidr := range strings.Split(*oklist, ",") {
			if cidrMatches(ip, strings.TrimSpace(cidr)) {
				return
			}
		}
		err = fmt.Errorf("output GRE port %s does not match any allowed CIDR in the configuration.", *port)
	}
	return
}

func validateOutputPort(port *string) (newport *string, err error) {
	if port == nil {
		err = fmt.Errorf("no output port specified.")
		return
	}
	if strings.HasPrefix(*port, "label:") {
		label := (*port)[6:]
		// check for a label with this name in the configuration
		mirsect := cfg_data["mirroring"]
		for k, v := range mirsect {
			if k == label {
				newport = v
				err = validateAllowedOutputIP(newport)
				return
			}
		}
		err = fmt.Errorf("output port label %s does not exist in the configuration.", label)
		return
	}
	if strings.Index(*port, "/") < 0 {
		if net.ParseIP(*port) != nil {
			// simple name or IP, assumed to be OK
			err = validateAllowedOutputIP(port)
			if err == nil {
				newport = port
			}
		} else {
			// need to map DNS name to IP addr
			addrs, err := net.LookupHost(*port)
			if addrs != nil && err == nil {
				err = validateAllowedOutputIP( &addrs[0] )
				if err == nil {
					newport = &addrs[0]
				}
			}
		}
		return
	}
	_, err = validatePort(port)
	return
}

/*
 * Handle a PUT request (not supported currently).
 */
func mirror_put( out http.ResponseWriter, data []byte ) (code int, msg string) {
	msg = "PUT /tegu/mirrors/ requests are unsupported"
	http_sheep.Baa( 1, msg )
	code = http.StatusMethodNotAllowed
	return
}

/*
 *	Parse and react to a POST to /tegu/mirrors/. We expect JSON describing the mirror request, to wit:
 *		{
 *			"start_time": "nnn",                 // optional
 *			"end_time": "nnn",                   // required
 *			"output": "<output spec>",           // required
 *			"port": [ "port1" , "port2", ...],   // required
 *			"vlan": "vlan",                      // optional
 *			"cookie": "value",                   // optional
 *			"name": "mirrorname",                // optional
 *		}
 *
 *	Because multiple mirrors may be created as a result, we return an array of JSON results, one for each mirror:
 *		[
 *		  {
 *			"name": "mirrorname",   // tegu or user-defined mirror name
 *			"url": "url",           // URL to use for DELETE or GET
 *			"error": "err"          // error message (if any)
 *		  },
 *		  ....
 *		]
 */
func mirror_post( in *http.Request, out http.ResponseWriter, data []byte ) (code int, msg string) {
	http_sheep.Baa( 5, "Request data: " + string(data))
	code = http.StatusOK

	// 1. Unmarshall the JSON request, check for required fields
	type req_type struct {
		Start_time	string	 `json:"start_time"`
		End_time	string	 `json:"end_time"`		// required
		Output		string	 `json:"output"`		// required
		Port 		[]string `json:"port"`			// required
		Vlan 		string	 `json:"vlan"`
		Cookie 		string	 `json:"cookie"`
		Name 		string	 `json:"name"`
	}
	var req req_type
	if err := json.Unmarshal(data, &req); err != nil {
		code = http.StatusBadRequest
		msg = "Bad JSON: " + err.Error()
		return
	}
	if req.End_time == "" || req.Output == "" || len(req.Port) == 0 {
		code = http.StatusBadRequest
		msg = "Missing a required field."
		return
	}

	// 2. Check start/end times, and VLAN list
	stime, etime, err := checkTimes(req.Start_time, req.End_time)
	if err != nil {
		code = http.StatusBadRequest
		msg = err.Error()
		return
	}
	err = validVlanList(req.Vlan)
	if err != nil {
		code = http.StatusBadRequest
		msg = err.Error()
		return
	}

	// 3. Generate random name if not given
	if req.Name == "" {
		req.Name = generateMirrorName()
	} else if !validName(req.Name) {
		code = http.StatusBadRequest
		msg = "Invalid mirror name: "+req.Name
		return
	}

	// 4. Validate input ports, and assign into groups
	plist, err := validatePorts(req.Port, req.Name)
	if err != nil {
		// no valid ports, give up
		code = http.StatusBadRequest
		msg = err.Error()
		return
	}

	// 5. Validate output port
	newport, err := validateOutputPort(&req.Output)
	if err != nil {
		code = http.StatusBadRequest
		msg = err.Error()
		return
	}
	req.Output = *newport

	// 6. Make one pledge per mirror, send to reservation mgr, build JSON return string
	scheme := "http"
	if (isSSL) {
		scheme = "https"
	}
	code = http.StatusCreated
	sep := "\n"
	bs := bytes.NewBufferString("[")
	for key, mirror := range *plist {
		if key != "_badports_" {
			// Make a pledge
			phost := key
			nam   := mirror.name
			res, err := gizmos.Mk_mirror_pledge( mirror.ports, &req.Output, stime, etime, &nam, &req.Cookie, &phost, &req.Vlan )
			if res != nil {
				req := ipc.Mk_chmsg( )
				my_ch := make( chan *ipc.Chmsg )					// allocate channel for responses to our requests
				defer close( my_ch )								// close it on return
				req.Send_req( rmgr_ch, my_ch, REQ_ADD, res, nil )	// network OK'd it, so add it to the inventory
				req = <- my_ch										// wait for completion

				if req.State == nil {
					ckptreq := ipc.Mk_chmsg( )
					ckptreq.Send_req( rmgr_ch, nil, REQ_CHKPT, nil, nil )	// request a chkpt now, but don't wait on it
				} else {
					err = fmt.Errorf( "%s", req.State )
				}

				if res_paused {
					rm_sheep.Baa( 1, "reservations are paused, accepted reservation will not be pushed until resumed" )
					res.Pause( false )								// when paused we must mark the reservation as paused and pushed so it doesn't push until resume received
					res.Set_pushed( )
				}

			} else {
				if err == nil {
					err = fmt.Errorf( "specific reason unknown" )						// ensure we have something for message
				}
				mirror.err = err
			}
		}

		bs.WriteString(fmt.Sprintf(`%s { "name": "%s", `, sep, mirror.name))
		bs.WriteString(fmt.Sprintf(`"port": [ `))
		sep2 := ""
		for _, p := range mirror.ports {
			bs.WriteString(fmt.Sprintf(`%s"%s"`, sep2, p))
			sep2 = ", "
		}
		bs.WriteString(fmt.Sprintf(` ], `))
		if mirror.err == nil {
			bs.WriteString(fmt.Sprintf(`"url": "%s://%s/tegu/mirrors/%s/"`, scheme, in.Host, mirror.name))
		} else {
			bs.WriteString(fmt.Sprintf(`"error": "%s"`, mirror.err.Error()))
		}
		bs.WriteString(" }")
		sep = ",\n"
	}
	bs.WriteString("\n]\n")
	msg = bs.String()
	return
}

/*
 * Handle a DELETE /tegu/mirrors/<name>/[?cookie=<cookie>] request.
 */
func mirror_delete( in *http.Request, out http.ResponseWriter, data []byte ) (code int, msg string) {
	name, cookie := getNameAndCookie(in)
	mirror := lookupMirror(name, cookie)
	if mirror == nil {
		code = http.StatusNotFound
		msg = "Not found."
		return
	}
	if ! mirror.Is_valid_cookie(&cookie) {
		code = http.StatusUnauthorized
		msg = "Unauthorized."
		return
	}

	req := ipc.Mk_chmsg( )
	my_ch := make( chan *ipc.Chmsg )					// allocate channel for responses to our requests
	defer close( my_ch )								// close it on return
	namepluscookie := []*string { &name, &cookie }
	req.Send_req( rmgr_ch, my_ch, REQ_DEL, namepluscookie, nil )	// remove the reservation
	req = <- my_ch										// wait for completion

	if req.State == nil {
		ckptreq := ipc.Mk_chmsg( )
		ckptreq.Send_req( rmgr_ch, nil, REQ_CHKPT, nil, nil )	// request a chkpt now, but don't wait on it
	}

	code = http.StatusNoContent
	msg = ""
	return
}

/*
 * Handle a GET /tegu/mirrors/ or GET /tegu/mirrors/<name>/[?cookie=<cookie>] request.
 * The first form lists all mirrors, the second form list details of one mirror.
 */
func mirror_get( in *http.Request, out http.ResponseWriter, data []byte ) (code int, msg string) {
	name, cookie := getNameAndCookie(in)
	scheme := "http"
	if (isSSL) {
		scheme = "https"
	}
	if name == "" {
		// List all mirrors
		list := getMirrors()
		sep := "\n"
		bs := bytes.NewBufferString("[")
		for _, s := range list {
			if s != "" {
				bs.WriteString(fmt.Sprintf(`%s { "name": "%s", "url": "%s://%s/tegu/mirrors/%s/" }`, sep, s, scheme, in.Host, s))
				sep = ",\n"
			}
		}
		bs.WriteString("\n]\n")
		code = http.StatusOK
		msg = bs.String()
	} else {
		mirror := lookupMirror(name, cookie)
		if mirror == nil {
			code = http.StatusNotFound
			msg = "Not found."
			return
		}
		if ! mirror.Is_valid_cookie(&cookie) {
			code = http.StatusUnauthorized
			msg = "Unauthorized."
			return
		}
		code = http.StatusOK
		msg = convertToJSON(mirror, scheme, in.Host)
	}
	return
}

/*
 *  All requests to the /tegu/mirrors/ URL subtree are funneled here for handling.
 */
func mirror_handler( out http.ResponseWriter, in *http.Request ) {
	code := http.StatusOK	// response code to return
	msg  := ""				// data to go in response (assumed to be JSON, if code = StatusOK or StatusCreated)

	data := dig_data( in )
	if data == nil {						// missing data -- punt early
		http_sheep.Baa( 1, "http: mirror_handler called without data: %s", in.Method )
		code = http.StatusBadRequest
		msg = "missing data"
	} else {
		http_sheep.Baa( 1, "Request from %s: %s %s", in.RemoteAddr, in.Method, in.RequestURI )
		switch in.Method {
			case "PUT":
				code, msg = mirror_put( out, data )

			case "POST":
				code, msg = mirror_post( in, out, data )

			case "DELETE":
				code, msg = mirror_delete( in, out, data )

			case "GET":
				code, msg = mirror_get( in, out, data )

			default:
				http_sheep.Baa( 1, "mirror_handler called for unrecognised method: %s", in.Method )
				code = http.StatusMethodNotAllowed
				msg = fmt.Sprintf( "unrecognised method: %s", in.Method )
		}
	}

	// set response code and write response
	if code == http.StatusOK || code == http.StatusCreated {
		// Set Content-type header for JSON
		hdr := out.Header()
		hdr.Add("Content-type", "application/json")
	} else {
		http_sheep.Baa( 1, "Response: " + msg)
	}
	out.WriteHeader(code)
	out.Write([]byte(msg))
}
