// vi: sw=4 ts=4:

/*
------------------------------------------------------------------------------------------------
	Mnemonic:	flight_if
	Abstract:	Interface to the floodlight environment (including skoogi).
	Date:		24 Octoberr 2013
	Authors:	E. Scott Daniels, Matti Hiltnuen, Kaustubh Joshi
------------------------------------------------------------------------------------------------
*/

package gizmos

import (
	"bytes"
	//"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	//"os"
	"strings"
)

/*
type host struct {
	ip []string				// list of ip addresses that the host has assigned
	swname []string			// list of switches that the host is attached to
}
*/

// ------------- structs that the json returned by floodlight maps to ----------------------
/*
	unfortunately the easiest way, from a coding perspective, to snarf and make use of the 
	json returned by floodlight's api is to define structs that match certain parts of the 
	json.  We only have to define fields/objects that we are insterested in, so additions
	to the json will likely not break things here, but if a field name that we are interested
	in changes we'll fall over. (I see this as a problem regardless of the language that 
	is used to implement this code and not a downfall of Go.) 
	
	The json parser will insert data only for fields that are externally visiable in these
	structs (capitalised first character).  The remainder of the field names match those 
	in the json data.  We can also insert non-exported fields which are unaffected by 
	the parser. 
	
	Bloody floodlight uses names that cannot be legally mapped to Go variable names (e.g.
	dst-port).  We (blindly for now) will convert all '-' characters followed by a lowercase
	alpha character to _. This will have the side effect of changing the actual data in 
	addition to the names, but I don't think that's an issue for this prototype and I 
	don't want to take the time to parse out the names and convert just those.  

	A side effect of using the in-built json functions of go is that all of the elements 
	of the structs must be externally accessable. 
*/

// ../wm/....flow/json; generates three types of structs

type FL_action_json struct {
		QueueId	int
		Port	int
		Type	string
}

type FL_match_json struct {
	DataLayerDestination	string
	DataLayerSource			string
	DataLayerType			int
	DataLayerVirtualLan		int
	NetworkProtocol			int
	NetworkDestination		string
	NetworkSource			string
	NetworkTypeOfService 	int
	TransportSource 		int
	InputPort				int
	DataLayerVirtualLanPriorityCodePoint int
}

type FL_flow_json struct {
	Cookie	float64
	Actions	[]FL_action_json
	Match	FL_match_json
}

// ...wm/device/, generates two structs
type FL_attachment_json struct {
	SwitchDPID	string
	Port		int
}

type FL_host_json struct {
	EntityClass string
	Mac		[]string
	Ipv4	[]string
	Ipv6	[]string					// haven't seen this, but it might be supported
	AttachmentPoint	[]FL_attachment_json
}

// ...wm/topology/links/json generates one struct
type FL_link_json struct {
	Src_switch string
	Src_port int
	Dst_switch string
	Dst_port int
	Type string
	Direction string
}

// -----------------------------------------------------------------------------------------


const (
	CVT_DASHES	bool = true
	NO_CVT		bool = false
)

// ---------------------------- private:  direct floodlight communications and response cracking ------------------------
// sends a get request to floodlight and extracts the resulting value if successful
// if cvt dashes is true, then the dash matching all /[a-z]-/ occurences will blindly be 
// converted to '_'.  See the note at the top with the other json issues
//
func get_flinfo( uri *string, cvt_dashes bool ) (jdata []byte, err error) {
	
	jdata = nil

	resp, err := http.Get( *uri )
	if err == nil {
		jdata, err = ioutil.ReadAll( resp.Body )
		resp.Body.Close( )

		if cvt_dashes  && err == nil {
			prev_alpha := false
			for i := range jdata {				// bloody floodlight uses unacceptable names (dash separated)
				if jdata[i] >= 'a'  &&  jdata[i] <= 'z' {
					prev_alpha = true
				} else {
					if prev_alpha && jdata[i] == '-' {
						jdata[i] = '_'
					}
					prev_alpha = false
				}
			}
		}
	}

	return
}

/*
	send a POST
*/
func post_flreq( uri *string ) (rstring string, err error) {
	resp, err := http.PostForm( *uri, url.Values{ } )		
	if( err == nil ) {
		rbytes, _ := ioutil.ReadAll( resp.Body )
		resp.Body.Close()
		rstring = string( rbytes )
	}

	return
}


// --------------------- public functions --------------------------------------------------

/*
	  make the necessary get api calls to floodlight (listening on host_port)
	  and build an array of host elements. 
	
	  we exepct the json from the fl call to be an array of "objects" of the form:
			entityClass = DefaultEntityClass
			lastSeen = 1382929191584.00
			mac[0] = ce:a8:5a:5e:a1:aa
			ipv4[0] = 10.0.0.7
			
			attachmentPoint[0]:				( array of struct )
				switchDPID = 00:00:00:00:00:00:00:07
				port = 1.00
				errorStatus = null/undefined
*/
func FL_hosts( host_port *string ) ( hlist []FL_host_json ) {
	hlist = nil;

	uri := fmt.Sprintf( "http://%s/wm/device/", *host_port )		// for some unknown reason, the trailing slant after dev is required
	jdata, err := get_flinfo( &uri, NO_CVT ) 
	if err != nil {
		obj_sheep.Baa( 0, "WRN: FL_hosts: error during api get call: %s", err )
		return
	}

	hlist = make( []FL_host_json, 4096 )
	err = json.Unmarshal( jdata, &hlist )			// unpack the json into the host list
	if err != nil {
		obj_sheep.Baa( 0, "WRN: FL_hosts: error during json unpack: %s", err )
		hlist = nil;
		return
	}

	return
}

/*
	make the necessary floodlight api calls to create a list of known links. 
	host_port is the host:port string where floodlight is listening.

	the json is assumed to be an array of a single object: 
		src-switch = 00:00:00:00:00:00:00:01
		src-port = 2.00
		dst-switch = 00:00:00:00:00:00:00:05
		dst-port = 3.00
		type = internal
		direction = bidirectional
*/
func FL_links( host_port *string ) ( llist []FL_link_json ) {


	uri := fmt.Sprintf( "http://%s/wm/topology/links/json", *host_port )
	jdata, err := get_flinfo( &uri, CVT_DASHES ) 
	if err != nil {
		obj_sheep.Baa( 0, "WRN: FL_links: error during api get call: %s", err )
		llist = nil
		return
	}


	llist = make( []FL_link_json, 4096 )
	err = json.Unmarshal( jdata, &llist )			// unpack the json into the host list
	if err != nil {
		obj_sheep.Baa( 0, "WRN: FL_links: error during json unpack: %s", err )
		llist = nil
		return
	}

	return
}

/*
	Sends an old (original) style reservaton to skoogi.
	/wm/skapi/txt"?action=phostadd&host1=$2&host2=$3&expiry=$4&queue=${5:-1}"
*/
func SK_reserve(  flhost *string, h1 string, h2 string, expiry int64, queue int ) ( err error ) { 
	var (
		uri	string
		body	*bytes.Buffer
		resp	*http.Response
	)

	body = bytes.NewBufferString( "no-data" )			// skoogi doesn't accept data yet; all parms tacked onto the url
	err = nil

	if h2 == "0.0.0.0" || h2 == "any" {
		uri = fmt.Sprintf( "%s/wm/skapi/txt?action=phostadd&host1=%s&expiry=%d&queue=%d", *flhost, h1, expiry, queue )			// make a single host request
	} else {
		uri = fmt.Sprintf( "%s/wm/skapi/txt?action=phostadd&host1=%s&host2=%s&expiry=%d&queue=%d", *flhost, h1, h2, expiry, queue )
	}

	obj_sheep.Baa( 1, "sending reservation to skoogi: %s", uri )
	resp, err = http.Post( uri, "plain/text", body )
	if err != nil {
		return
	}
	defer resp.Body.Close()

	rbody, err := ioutil.ReadAll( resp.Body )
	if err == nil {
		obj_sheep.Baa( 1, "SKreserve: skoogi response: %s", rbody )
	} else {
		obj_sheep.Baa( 0, "SKreserve: skoogi request error: %s", err )
	}

	return
}

/*
	Sends an ingress/egress flow-mod add request to skoogi. 
	/wm/skapi/txt"?action=iefmadd&srchhost=$2&desthost=<host>&expiry=<host>&queue=<qnum>&swid=<switch>&port=<port>"
*/
func SK_ie_flowmod(  flhost *string, srchost string, desthost string, expiry int64, queue int, swid string, port int ) ( err error ) { 
	var (
		uri	string
		body	*bytes.Buffer
		resp	*http.Response
	)

	body = bytes.NewBufferString( "no-data" )			// skoogi doesn't accept data yet; all parms tacked onto the url
	err = nil

	if strings.Index( swid, ":" ) > 0 {					// must remove colons if they are there 
		tokens := strings.Split( swid, ":" )
		swid = strings.Join( tokens, "" )
	}

	//if desthost == "0.0.0.0" || desthost == "any" {
	if desthost == "any" {
		//uri = fmt.Sprintf( "%s/wm/skapi/txt?action=iefmadd&srchost=%s&expiry=%d&queue=%d", *flhost, h1, expiry, queue )			// make a single host request
		obj_sheep.Baa( 0, "ERR: SK_ie_flowmod: cannot add an ie flowmod for just one host" )
		err = fmt.Errorf( "cannot set an ie flowmod for just one host; requires srchost, desthost pair" )
		return
	} else {
		uri = fmt.Sprintf( "%s/wm/skapi/txt?action=iefmadd&srchost=%s&desthost=%s&expiry=%d&queue=%d&swid=%s&port=%d", *flhost, srchost, desthost, expiry, queue, swid, port )
	}

	obj_sheep.Baa( 2, "sk_ie_flomod: sending ie reservation to skoogi: %s", uri )
	resp, err = http.Post( uri, "plain/text", body )
	if err != nil {
		return
	}
	defer resp.Body.Close()

	rbody, err := ioutil.ReadAll( resp.Body )
	if err == nil {
		obj_sheep.Baa( 2, "SK_ie_flomod: skoogi response: %s", rbody )
	} else {
		obj_sheep.Baa( 0, "SK_ie_flomod: ERR: skoogi request failed: %s", err )
	}

	return
}
