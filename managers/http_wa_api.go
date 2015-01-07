// vi: sw=4 ts=4:

/*

	Mnemonic:	http_wa_api
	Abstract:	These are the functions that support the wide area interface that Tegu supplies.
				Because WACC wanted a more true to form ReST bit of goo, each function supports
				one path down the URI and so there's a lot of duplicated code; sigh.

				CAUTION:
				The 'handler' functions are called as goroutines and thus will run concurrently!
				return from the function indicates end of processing and the http interface will
				'close' the transaction.

	Date:		05 January 2015
	Author:		E. Scott Daniels

	Mods:		
*/

package managers

import (
	"encoding/json"
	"fmt"
	//"io/ioutil"
	"net/http"
	//"os"
	//"strings"
	//"syscall"
	//"time"

	//"codecloud.web.att.com/gopkgs/bleater"
	//"codecloud.web.att.com/gopkgs/clike"
	//"codecloud.web.att.com/gopkgs/token"
//	"codecloud.web.att.com/gopkgs/ipc"
	//"codecloud.web.att.com/gopkgs/security"

	//"codecloud.web.att.com/tegu/gizmos"
)


// --- wa request/response structs ------------------------------------------------------------------

/*
	Request and response structs. Fields are public so that we can use the json (un)marhsal
	calls to bundle and unbundle the data. Tags are needed to support the WACC (java?) camel
	case that doesn't have a leading capitalised letter.
	
	The structs contain information that is expected to be received from WACC in json form
	and contain internal information that is needed when passing the data into the agent manager
	for acutal execution.
*/
type wa_port_req struct {
	Token	string
	Tenant	string 		// uuid		
	Subnet	string 		// uuid

	host	string		// tegu private information
}

type wa_port_resp struct {
	Tenant	string		`json:"tenant";`
	Router	string		`json:"router";`
	IP		string		`json:"ip";`
}

type wa_tunnel_req struct {
	Local_tenant	string	`json:"localTenant";`		// uuid
	Local_router	string	`json:"localRouter";`		// uuid
	Local_ip		string	`json:"localIp";`
	Remote_ip		string	`json:"remoteIp";`
	Bandwidth		string	`json:"bandwidth";`

	host			string		// tegu private information
}

type wa_tunnel_resp struct {
	Tenant		string	`json:"tenant";`
	Router		string	`json:"router";`
	Ip			string	`json:"ip";`
	Cidr		string	`json:"cidr";`
	Bandwidth	string	`json:"bandwidth";`
}

type wa_route_req struct {
	Local_tenant	string	`json:"localTenant";`
	Local_router	string	`json:"localRouter";`
	Local_ip		string	`json:"localIp";`
	Remote_ip		string	`json:"remoteIp";`
	Remote_cidr 	string	`json:"remoteCidr";`
	Bandwidth		string	`json:"bandwidth";`

	host			string		// tegu private information
}

// ---- request specific functions ------------------------------------------------------------------
/* Generate a hash of parameter things from the structure */
func (r *wa_port_req) To_map( ) ( map[string]string ) {
	if r == nil {
		return nil
	}

	m := make( map[string]string )
	m["token"] = r.Token
	m["tenant"] = r.Tenant
	m["subnet"] = r.Subnet

	return m
}

/* Generate a hash of parameter things from the structure */
func (r *wa_tunnel_req) To_map( ) ( map[string]string ) {
	if r == nil {
		return nil
	}

	m := make( map[string]string )
	m["localtenant"] = r.Local_tenant
	m["localrouter"] = r.Local_router
	m["localip"] = r.Local_ip
	m["remoteip"] = r.Remote_ip
	m["bandwidth"] = r.Bandwidth

	return m
}

/* Generate a hash of parameter things from the structure */
func (r *wa_route_req) To_map( ) ( map[string]string ) {
	if r == nil {
		return nil
	}

	m := make( map[string]string )
	m["localtenant"] = r.Local_tenant
	m["localrouter"] = r.Local_router
	m["localip"] = r.Local_ip
	m["remoteip"] = r.Remote_ip
	m["remote_cidr"] = r.Remote_cidr
	m["bandwidth"] = r.Bandwidth

	return m
}

// --------------------------------------------------------------------------------------------------

/*
	Generic data digger for wa functions.  Pulls the data and then unbundles the json into the 
	structure passed in.  Returns a state sutible for using as the response header if there is an
	error (html.StatusOK if all was good) and a reason string.
*/
func wa_dig_data( in *http.Request, request interface{} ) ( state int, reason string ) {
	state = http.StatusBadRequest
	reason = ""

	data := dig_data( in )
	if( data == nil ) {						// missing data -- punt early
		reason = `{ "reason": "missing data" }`
		http_sheep.Baa( 1, "http_wa_api: called without data: %s", in.Method )
		return 
	}
	
	err := json.Unmarshal( data, &request )           // unpack the json 
	if err != nil {
		reason = `{ "reason": "bad json request" }`
		http_sheep.Baa( 1, "http_wa_api: json format error: %s", err )
		//http_sheep.Baa( 1, ">>>> http_wa_api: raw-json: %s", data )
		return
	}

	state = http.StatusOK
	return
}

/*
	Handle tegu/rest/ports  api call.  
*/
func http_wa_ports( out http.ResponseWriter, in *http.Request ) {
	var (
		state	= http.StatusMethodNotAllowed
		reason	string
	)

	request := &wa_port_req{}							// empty request for dig_data to fill

	state, reason = wa_dig_data( in, request )
	if state != http.StatusOK {
		out.Header().Set( "Content-Type", "application/json" )
		out.WriteHeader( state )
		fmt.Fprintf( out, "%s\n", reason )
		return
	}

	switch in.Method {
		case "POST":
			http_sheep.Baa( 0, ">>>> received POST: ten=%s  subnet=%s\n", request.Tenant, request.Subnet )
/*
			my_ch := make( chan *ipc.Chmsg )								// channel to wait for response from agent

			//TODO: send request off to agent and wait

			msg := ipc.Mk_chmsg( )
			msg.Send_req( am_ch, my_ch, REQ_WA_PORT, request, nil )			// send request to agent and block 
			msg = <- my_ch
			
			if msg != nil {
				state = http.StatusCreated
				output := msg.Response_data.( []string )						// a collection of records from the stdout
				if len( output ) > 0  {
					reason = output[0]
				} else {
					reason = ""
				}
			} else {
				state = http.StatusInternalServerError
				reason = "missing or no response from agent"
			}
*/
			reason = `{ "tenant": "3ec3f998-c720-49e6-a729-941af4396f7a", "router": "de854701-7b80-4f31-a2e4-f4ad1a988627", "ip": "135.207.50.100" }` 

			state = http.StatusCreated

		default:
			http_sheep.Baa( 1, "http_wa_ports: called for unrecognised method: %s", in.Method )
			reason = fmt.Sprintf( `{ "reason": "%s request method not supported" }`, in.Method )
			state = http.StatusMethodNotAllowed
	}

	out.Header().Set( "Content-Type", "application/json" ) // must set type and force write with state before writing data
	out.WriteHeader( state )		
	if state != http.StatusOK {
		if reason == "" {
			reason = `{ "reason": "bad json request" }`
		}
	} 

	fmt.Fprintf( out, "%s\n", reason )
	return
}

/*
	Handle tegu/rest/tunnel  api call.  
*/
func http_wa_tunnel( out http.ResponseWriter, in *http.Request ) {
	var (
		state	= http.StatusMethodNotAllowed
		reason	string
	)

	request := &wa_tunnel_req{ }							// empty request for dig_data to fill

	state, reason = wa_dig_data( in, request )
	if state != http.StatusOK {
		out.Header().Set( "Content-Type", "application/json" )
		out.WriteHeader( state )
		fmt.Fprintf( out, "%s\n", reason )
		return
	}

	switch in.Method {
		case "POST":
			//TODO: send request off to agent and wait

			state = http.StatusCreated
			reason = ` {"localTenant": "3ec3f998-c720-49e6-a729-941af4396f7a", "localRouter": "de854701-7b80-4f31-a2e4-f4ad1a988627", "localIp": "135.207.50.100", "remoteIp": "135.207.50.101", "bandwidth": "1000"}`

		default:
			http_sheep.Baa( 1, "http_wa_tunnel: called for unrecognised method: %s", in.Method )
			reason = fmt.Sprintf( `{ "reason": "%s request method not supported" }`, in.Method )
			state = http.StatusMethodNotAllowed
	}

	out.Header().Set( "Content-Type", "application/json" ) 		// must set type and force write with state before writing data
	out.WriteHeader( state )		
	if state != http.StatusOK {
		if reason == "" {
			reason = `{ "reason": "bad json request" }`
		}
	} 

	fmt.Fprintf( out, "%s\n", reason )

	return
}

/*
	Handle tegu/rest/route  api call.  
*/
func http_wa_route( out http.ResponseWriter, in *http.Request ) {
	var (
		state	= http.StatusMethodNotAllowed
		reason	string
	)

	request := &wa_route_req{ }							// empty request for dig_data to fill

	state, reason = wa_dig_data( in, request )
	if state != http.StatusOK {
		out.Header().Set( "Content-Type", "application/json" )
		out.WriteHeader( state )
		fmt.Fprintf( out, "%s\n", reason )
		return
	}

	switch in.Method {
		case "POST":
			//TODO: send request off to agent and wait

			state = http.StatusNoContent
			reason = ""

		default:
			http_sheep.Baa( 1, "http_wa_route: called for unrecognised method: %s", in.Method )
			reason = fmt.Sprintf( `{ "reason": "%s request method not supported" }`, in.Method )
			state = http.StatusMethodNotAllowed
	}

	out.Header().Set( "Content-Type", "application/json" )
	out.WriteHeader( state )		// must lead with the overall state, followed by reason or data
	if state != http.StatusOK {
		if reason == "" {
			reason = `{ "reason": "bad json request" }`
		}
	} 

	fmt.Fprintf( out, "%s\n", reason )

	return
}

