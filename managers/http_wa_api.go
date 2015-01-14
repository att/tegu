// vi: sw=4 ts=4:

/*

	Mnemonic:	http_wa_api
	Abstract:	These are the functions that support the wide area ReST interface that Tegu supplies.
				Because WACC wanted a more true to form ReST bit of goo, each function supports
				one path down the URI and so there's probably a lot of duplicated code; sigh.

				CAUTION:
				The 'handler' functions are called as goroutines and thus will run concurrently!
				return from the function indicates end of processing and the http interface will
				'close' the transaction.

	Date:		05 January 2015
	Author:		E. Scott Daniels

	Mods:		13 Jan 2015 - Added delete support
*/

package managers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"codecloud.web.att.com/gopkgs/token"
	"codecloud.web.att.com/gopkgs/ipc"
)


// --- wa request/response structs ------------------------------------------------------------------

/*
	Request structs. Fields are public so that we can use the json (un)marhsal calls to 
	bundle and unbundle the data. Tags are needed to support the WACC (java?) camel case 
	that doesn't have a leading capitalised letter.
	
	The structs contain information that is expected to be received from WACC in json form
	and contain internal information that is needed when passing the data into the agent manager
	for acutal execution.
*/
type wa_port_req struct {
	Token	string
	Tenant	string 		// uuid		
	Subnet	string 		// uuid

	host	*string		// tegu private information
}

type wa_tunnel_req struct {
	Local_tenant	string	`json:"localTenant";`		// uuid
	Local_router	string	`json:"localRouter";`		// uuid
	Local_ip		string	`json:"localIp";`
	Remote_ip		string	`json:"remoteIp";`
	Bandwidth		string	`json:"bandwidth";`			// optional

	host			*string		// tegu private information
}

type wa_route_req struct {
	Local_tenant	string	`json:"localTenant";`
	Local_router	string	`json:"localRouter";`
	Local_ip		string	`json:"localIp";`
	Remote_ip		string	`json:"remoteIp";`
	Remote_cidr 	string	`json:"remoteCidr";`
	Bandwidth		string	`json:"bandwidth";`

	host			*string		// tegu private information
}

type wa_conns_req struct {
	Token		string
	Tenant		string
	Router		string
	Subnet		string
	Remote_cidr	string		`json:"remoteCidr";`
	Wan_uuid	string
	Tos			string					// optional (future)

	host		*string
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

/* Generate a hash of parameter things from the structure */
func (r *wa_conns_req) To_map( ) ( map[string]string ) {
	if r == nil {
		return nil
	}

	m := make( map[string]string )
	m["tenant"] = r.Tenant
	m["token"] = r.Token
	m["router"] = r.Router
	m["subnet"] = r.Subnet
	m["wan_uuid"] = r.Wan_uuid
	m["remote_cidr"] = r.Remote_cidr
	m["tos"] = r.Tos

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
		reason = "missing data"
		http_sheep.Baa( 1, "http_wa_api: called without data: %s", in.Method )
		return 
	}
	
	http_sheep.Baa( 2, "http_wa_api: raw-json: %s", data )
	err := json.Unmarshal( data, &request )           // unpack the json 
	if err != nil {
		reason = "bad json request"
		http_sheep.Baa( 1, "http_wa_api: json format error: %s", err )
		return
	}

	state = http.StatusOK
	return
}

func send_http_err( out http.ResponseWriter, state int, msg string ) {
	http_sheep.Baa( 1, "unable to complete http request: %s", msg )

	//out.Header().Set( "Content-Type", "application/json" )
	out.Header().Set( "Content-Type", "text/pain" )
	out.WriteHeader( state )
	fmt.Fprintf( out, `{ "reason": %q }`, msg )
}


/*
	Given a project id and subnet id, return the block of subnet info.
*/
func http_subnet_info( project *string, subnet *string ) ( si *Subnet_info, err error ) {
	my_ch := make( chan *ipc.Chmsg )								// channel for responses (osif and agent requests)

	err = nil

	msg := ipc.Mk_chmsg( )
	msg.Send_req( osif_ch, my_ch, REQ_GET_SNINFO, *project + " " +  *subnet, nil )	// ask osif to dig up the info
	msg = <- my_ch													// block until we get a response
	if msg.State != nil {
		err = fmt.Errorf( "unable to get subnet information: %s: %s", *subnet, msg.State )
		return
	} else {
		if msg.Response_data == nil {
			err = fmt.Errorf( "subnet_info: unable to get subnet information: no data, no error" )
			return
		}
	}

	return msg.Response_data.( *Subnet_info ), err
}

/*
	Given a project id and subnet id, return the host that the associated gateway (router) is on, or 
	return err. 
*/
func http_subnet2gwhost( project *string, subnet *string ) ( host *string, err error ) {
	si, err := http_subnet_info( project, subnet )
	if err == nil {
		host = si.phost
	}

	return
}

/*
	Given a project id and a gateway (router) id return the physical host it lives on.
*/
func http_gw2phost( project *string, router *string ) ( host *string, err error ) {
	my_ch := make( chan *ipc.Chmsg )								// channel for responses (osif and agent requests)

	host = nil
	err = nil

	msg := ipc.Mk_chmsg( )
	if *project == "" {
		http_sheep.Baa( 1, "gw2phost: project was empty" )
		err = fmt.Errorf( "gw2phost: project string was missing" )
		return
	}
	if *router == "" {
		http_sheep.Baa( 1, "gw2phost: router was empty" )
		err = fmt.Errorf( "gw2phost: router string was missing" )
		return
	}

	msg.Send_req( osif_ch, my_ch, REQ_GW2PHOST, *project + " " +  *router, nil )	// ask osif to dig up the info
	msg = <- my_ch													// block until we get a response
	if msg.State != nil {
		err = fmt.Errorf( "gw2phost: unable to get router phost information: %s %s", *router,  msg.State )
		return
	} else {
		if msg.Response_data == nil {
			err = fmt.Errorf( "unable to get router information: no data, no error" )
			return
		}
	}

	h := msg.Response_data.( string )
	host = &h

	return
}


// - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - -

/*	Handle tegu/rest/ports  api call.  
	The http interface is the point where the inbound request is unpacked into a struct
	that can be passed to the agent manager, and then for taking the response back from 
	the agent and converting it to the output format reqired by the requestor.  There might
	be different output formats generated based on the bloody REST URI etc, so this is the 
	right place to do it (not in the agent, or agent manager).
*/
func http_wa_ports( out http.ResponseWriter, in *http.Request ) {
	var (
		state	= http.StatusMethodNotAllowed
		data	string
	)

	request := &wa_port_req{}							// empty request for dig_data to fill

	state, reason := wa_dig_data( in, request )
	if state != http.StatusOK {
		send_http_err( out, state, reason )
		return
	}

	switch in.Method {
		case "POST":
			state = http.StatusCreated
			var err		error
			http_sheep.Baa( 1, "wa_ports received POST: ten=%s  subnet=%s\n", request.Tenant, request.Subnet )
			my_ch := make( chan *ipc.Chmsg )								// channel for responses (osif and agent requests)

			si, err := http_subnet_info( &request.Tenant, &request.Subnet ) 			// suss out subnet info to get host and cidr
			if err != nil {
				http_sheep.Baa( 1, "wa_port: couldn't dig subnet info: %s", err )
				send_http_err( out, http.StatusInternalServerError, fmt.Sprintf( "wa_port/post: %s", err ) )
				return
			} 

			cidr := si.cidr
			request.host = si.phost
			request.Token = *si.token	// must send our token in for admin privs

			msg := ipc.Mk_chmsg( )
			msg.Send_req( am_ch, my_ch, REQ_WA_PORT, request, nil )			// send request to agent and block 
			msg = <- my_ch
			
			if msg != nil {
				if msg.State == nil {										// success if no state
					state = http.StatusCreated
					output := msg.Response_data.( []string )						// a collection of records from the stdout
					if len( output ) > 0  {													// expected output is: router, port-id, ipaddress on a single line
						ntokens, otokens := token.Tokenise_qpopulated( output[0], " " )		// doesn't stumble over multiple whitespace between tokens like strings.Split does
						if ntokens >= 3 {
							if cidr == nil {
								dup_str := "0.0.0.0/24"
								cidr = &dup_str
							}

							// CAUTION: output from script is router-uuid, port-uuid, ip, but WACC wants only router and ip back (0 and 2)
							data = fmt.Sprintf( `{ "tenant": %q, "router": %q, "ip": %q, "cidr": %q }`, request.Tenant, otokens[0], otokens[2], *cidr )
						} else {
							data = fmt.Sprintf( "wa_port response wasn't correct: %d tokens (expected 3); %d lines (required 1)", ntokens, len( output ) )
							state = http.StatusInternalServerError
						}
					} else {
						data = ""
					}
				} else {
					http_sheep.Baa( 1, "wa_ports request failed: %s", msg.State )
					state = http.StatusInternalServerError
					data = fmt.Sprintf( "wa_port/post: script failed: %s", msg.State )
				}
			} else {
				state = http.StatusInternalServerError
				data = "wa_port/post: missing or no response from agent"
			}
			//data = `{ "tenant": "3ec3f998-c720-49e6-a729-941af4396f7a", "router": "de854701-7b80-4f31-a2e4-f4ad1a988627", "ip": "135.207.50.100" }` 

		default:
			http_sheep.Baa( 1, "http_wa_ports: called for unrecognised method: %s", in.Method )
			data = fmt.Sprintf( "%s request method not supported", in.Method )
			state = http.StatusMethodNotAllowed
	}

	if state > 299 { 
		out.Header().Set( "Content-Type", "text/plain" ) 			// must set type and force write with state before writing data
		data = fmt.Sprintf( `{ "reason": %q }`, data )
	} else {
		out.Header().Set( "Content-Type", "application/json" ) 
	}
	out.WriteHeader( state )		
	if state > 299 && data == "" {
		data = "bad json request"
	} 

	http_sheep.Baa( 1, "wa_port finished: %d: %s", state, data )
	fmt.Fprintf( out, "%s", data )
	return
}

/*
	Handle tegu/rest/tunnel  api call.  This is a bit different than the wa_port call inasmuch
	as we have to add some output to what comes back (nothing) from the underlying command.
*/
func http_wa_tunnel( out http.ResponseWriter, in *http.Request ) {
	var (
		state	= http.StatusMethodNotAllowed
		data	string
	)

	request := &wa_tunnel_req{ }							// empty request for dig_data to fill

	state, reason := wa_dig_data( in, request )
	if state != http.StatusOK {
		send_http_err( out, state, reason )
		return
	}

	state = http.StatusCreated				// we'll assume it works out
	switch in.Method {
		case "POST":
			var err		error

			http_sheep.Baa( 1, "wa_tunnel received POST: router=%s  ten=%s\n", request.Local_router, request.Local_tenant )

			request.host, err = http_gw2phost( &request.Local_tenant, &request.Local_router )
			if err != nil {
				send_http_err( out, http.StatusInternalServerError, fmt.Sprintf( "wa_tunnel/post: %s",  err ) )
				return
			}

			my_ch := make( chan *ipc.Chmsg )								// channel to wait for response from agent
			msg := ipc.Mk_chmsg( )
			msg.Send_req( am_ch, my_ch, REQ_WA_TUNNEL, request, nil )			// send request to agent and block 
			msg = <- my_ch
			
			if msg != nil  {
				if msg.State == nil {
					data = fmt.Sprintf( `{ "tenant": %q, "router": %q, "ip": %q`, request.Local_tenant, request.Local_router, request.Local_ip )
					if request.Bandwidth != "" {
						data += fmt.Sprintf( `, "bandwidth": %q }`, request.Bandwidth )
					} else {
						data += " }"
					}
				} else {
					state = http.StatusInternalServerError
					data = fmt.Sprintf( "wa_tunnel/post: script failed: %s", msg.State )
				}
			} else {
				state = http.StatusInternalServerError
				data = "wa_tunnel/post: missing or no response from agent"
			}

			//data = `{ "tenant": "3ec3f998-c720-49e6-a729-941af4396f7a", "router": "de854701-7b80-4f31-a2e4-f4ad1a988627", "ip": "135.207.50.100", "cidr": "192.168.1.0/24", "bandwidth": "1000"}`


		default:
			http_sheep.Baa( 1, "http_wa_tunnel: called for unrecognised method: %s", in.Method )
			data = fmt.Sprintf( "%s request method not supported", in.Method )
			state = http.StatusMethodNotAllowed
	}

	//out.Header().Set( "Content-Type", "application/json" ) 		// must set type and force write with state before writing data
	if state > 299 { 
		out.Header().Set( "Content-Type", "text/plain" ) 			// must set type and force write with state before writing data
		data = fmt.Sprintf( `{ "reason": %q }`, data )
	} else {
		out.Header().Set( "Content-Type", "application/json" ) 
	}
	out.WriteHeader( state )		
	if state > 299 && data == "" {
		data = "bad json request"
	} 

	http_sheep.Baa( 1, "wa_tunnel finished: %d: %s", state, data )
	fmt.Fprintf( out, "%s\n", data )

	return
}

/*
	Handle tegu/rest/route  api call.  
*/
func http_wa_route( out http.ResponseWriter, in *http.Request ) {
	var (
		state	= http.StatusMethodNotAllowed
		data	string
	)

	request := &wa_route_req{ }							// empty request for dig_data to fill

	state, data = wa_dig_data( in, request )
	if state != http.StatusOK {
		send_http_err( out, state, data )
		return
	}

	switch in.Method {
		case "POST":

			state = http.StatusNoContent
			data = ""
			var err		error
			request.host, err = http_gw2phost( &request.Local_tenant, &request.Local_router )
			if err != nil {
				send_http_err( out, http.StatusInternalServerError, fmt.Sprintf( "wa_route/post: %s", err ) )
				return
			}

			http_sheep.Baa( 1, "wa_route received POST: host=%s", *request.host )
			my_ch := make( chan *ipc.Chmsg )								// channel to wait for response from agent
			msg := ipc.Mk_chmsg( )
			msg.Send_req( am_ch, my_ch, REQ_WA_ROUTE, request, nil )		// send request to agent and block 
			msg = <- my_ch

			if msg != nil {
				if msg.State == nil {
					state = http.StatusNoContent
					data = ""
				} else {
					state = http.StatusInternalServerError
					data = fmt.Sprintf( "wa_route/post: script failed: %s",  msg.State )
				}
			} else {
				state = http.StatusInternalServerError
				data = "wa_tunnel/post: no response from agent"
			}

		default:
			http_sheep.Baa( 1, "http_wa_route: called for unrecognised method: %s", in.Method )
			data = fmt.Sprintf( "%s request method not supported", in.Method )
			state = http.StatusMethodNotAllowed
	}

	if state > 299 { 
		out.Header().Set( "Content-Type", "text/plain" ) 			// must set type and force write with state before writing data
		data = fmt.Sprintf( `{ "reason": %q }`, data )
	} else {
		out.Header().Set( "Content-Type", "application/json" ) 
	}
	//out.Header().Set( "Content-Type", "application/json" )
	out.WriteHeader( state )		// must lead with the overall state, followed by reason or data
	if state > 299 && data == "" {
		data = "bad json request"
	} 

	http_sheep.Baa( 1, "wa_route finished: %d: %s", state, data )
	fmt.Fprintf( out, "%s\n", data )

	return
}


/*	Handle tegu/rest/connections  api calls.  
*/
func http_wa_conn( out http.ResponseWriter, in *http.Request ) {
	var (
		state	= http.StatusMethodNotAllowed
		data	string
	)

	request := &wa_conns_req{}							// empty request for dig_data to fill

	state, reason := wa_dig_data( in, request )
	if state != http.StatusOK {
		send_http_err( out, state, reason )
		return
	}

	switch in.Method {
		case "DELETE":
			var err		error

			state = http.StatusCreated
			http_sheep.Baa( 1, "wa_ports received DELETE: ten=%s  router=%s wan=%s subnet=%s\n", request.Tenant, request.Router, request.Wan_uuid, request.Subnet )
			my_ch := make( chan *ipc.Chmsg )								// channel for responses (osif and agent requests)

			si, err := http_subnet_info( &request.Tenant, &request.Subnet ) 			// suss out subnet info to get host
			if err != nil {
				http_sheep.Baa( 1, "wa_conns: couldn't dig host info: ten=%s subnet=%s %s", request.Tenant, request.Subnet, err )
				send_http_err( out, http.StatusInternalServerError, fmt.Sprintf( "wa_conns/del %s", err ) )
				return
			} 

			request.host = si.phost
			request.Token = *si.token				// must send our token in for admin privs

			msg := ipc.Mk_chmsg( )
			msg.Send_req( am_ch, my_ch, REQ_WA_DELCONN, request, nil )			// send request to agent and block 
			msg = <- my_ch

			if msg != nil {
				if msg.State == nil {
					state = http.StatusNoContent
					data = ""
				} else {
					state = http.StatusInternalServerError
					data = fmt.Sprintf( "swa_conns/del: cript failed: %s",  msg.State )
				}
			} else {
				state = http.StatusInternalServerError
				data = "wa_conns/del: no response from agent"
			}

		default:
			http_sheep.Baa( 1, "http_wa_ports: called for unrecognised method: %s", in.Method )
			data = fmt.Sprintf( "%s request method not supported", in.Method )
			state = http.StatusMethodNotAllowed
	}

	if state > 299 { 
		out.Header().Set( "Content-Type", "text/plain" ) 			// must set type and force write with state before writing data
		data = fmt.Sprintf( `{ "function": "wa_conns", "reason": %q }`, data )
	} else {
		out.Header().Set( "Content-Type", "application/json" ) 
	}

	out.WriteHeader( state )		

	http_sheep.Baa( 1, "wa_port finished: %d: %s", state, data )
	fmt.Fprintf( out, "%s", data )
	return
}
