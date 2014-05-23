// vi: sw=4 ts=4:

/*

	Mnemonic:	http_api
	Abstract:	This provides an api interface based on http (shudders) RESTish. 
				The main method here is expected to be driven as a go routine from 
				the main tegu function.

				The main work functions (parse_get, parse_post, parse_delete) all generate
				json formatted data to the output device (we assume back to the requesting
				browser/user-agent).  The output should be an array (reqstate) with one "object" describing 
				the result of each request, and a final object (endstate) describing the overall state. 

	Date:		20 November 2013 (broken out of initial test on 2 Dec)
	Author:		E. Scott Daniels

	Mods:		05 May 2014 : Added agent manager to the verbose change list.
				13 May 2014 : Added support for exit-dscp value in reservation.
				22 May 2014 : Now forces a checkpoint after a successful reservation.
*/

package managers

import (
	//"bufio"
	//"encoding/json"
	//"flag"
	"fmt"
	"io/ioutil"
	//"html"
	"net/http"
	"os"
	"strings"
	"time"

	"forge.research.att.com/gopkgs/bleater"
	"forge.research.att.com/gopkgs/clike"
	"forge.research.att.com/gopkgs/token"
	"forge.research.att.com/gopkgs/ipc"
	"forge.research.att.com/tegu/gizmos"
)

/* ------------------------------------------------------------------------------------------------------ */

/*
	Make a reservation name that should be unique across invocations of tegu.
*/
func mk_resname( ) ( string ) {
	r := res_nmseed
	res_nmseed++
	return fmt.Sprintf( "res%x_%05d", pid, r ); 
}


// ------------------------------------------------------------------------------------------------------ 
/*
	pull the data from the request (the -d stuff from churl -d)
*/
func dig_data( resp *http.Request ) ( data []byte ) {
	data, err := ioutil.ReadAll( resp.Body )
	resp.Body.Close( )
	if( err != nil ) {
		http_sheep.Baa( 1, "unable to dig data from the request: %s", err )
		data = nil
	}

	return
}

/*
	parse and react to a POST request. we expect multiple, newline separated, requests
	to be sent in the body. Supported requests:

		ckpt
		listhosts
		listres
		listconns
		reserve <bandwidth[K|M|G][,outbandwidth[K|M|G]> [<start>-]<end> <host1>[-<host2] [cookie]
		graph
		ping
		listconns <hostname|hostip>
*/
func parse_post( out http.ResponseWriter, recs []string ) (state string, msg string) {
	var (
		err			error
		res			*gizmos.Pledge
		res_name	string = "undefined"
		tokens		[]string
		ntokens		int
		nerrors 	int = 0
		reason		string					// reason for the current status
		jreason		string					// json details from the pledge
		startt		int64
		endt		int64
		bandw_in	int64
		bandw_out	int64
		req_count	int = 0;				// number of requests attempted
		h1			string
		h2			string
		sep			string = ""				// json object separator
		req			*ipc.Chmsg
		my_ch		chan *ipc.Chmsg
	)


	my_ch = make( chan *ipc.Chmsg )							// allocate channel for responses to our requests
	defer close( my_ch )

	fmt.Fprintf( out,  "\"reqstate\": [ " )				// wrap request output into an array

	state = "OK"
	for i := 0; i < len( recs ); i++ {
		ntokens, tokens = token.Tokenise_qpopulated( recs[i], " " )		// split and keep populated tokens (treats successive sep chrs as one), preserves spaces in "s

		if ntokens < 1 || tokens[0][0:1] == "#" {
			continue
		}

		req_count++
		state = "ERROR"				// default for each loop; final set based on error count following loop
		jreason = ""
		reason = ""
		http_sheep.Baa( 3, "processing request: %s", tokens[0] )
		switch tokens[0] {

			case "ping":
				jreason = fmt.Sprintf( "\"pong: %s\"", version )
				state = "OK"

			case "chkpt":
				req = ipc.Mk_chmsg( )
				req.Send_req( rmgr_ch, nil, REQ_CHKPT, nil, nil )
				state = "OK"
				reason = "checkpoint was requested"

			case "graph":
				req = ipc.Mk_chmsg( )

				req.Send_req( nw_ch, my_ch, REQ_NETGRAPH, nil, nil )	// request to net thread; it will create a json blob and attach to the request which it sends back
				req = <- my_ch											// hard wait for network thread response
				if req.Response_data != nil {
					state = "OK"
					jreason = string( req.Response_data.(string) )
				} else {
					nerrors++
					jreason = ""
					reason = "no output from network thread"
				}
	
			case "listhosts":											// list known host information
				req = ipc.Mk_chmsg( )
				req.Send_req( nw_ch, my_ch, REQ_HOSTLIST, nil, nil )
				req = <- my_ch
				if req.State == nil {
					state = "OK"
					jreason = string( req.Response_data.(string) )
				} else {
					state = "ERROR"
					reason = fmt.Sprintf( "%s", req.State )
					jreason = ""
					nerrors++
				}
			
			case "listres":											// list reservations
				req = ipc.Mk_chmsg( )
				req.Send_req( rmgr_ch, my_ch, REQ_LIST, nil, nil )
				req = <- my_ch
				if req.State == nil {
					state = "OK"
					jreason = string( req.Response_data.(string) )
				} else {
					state = "ERROR"
					reason = fmt.Sprintf( "%s", req.State )
					jreason = ""
					nerrors++
				}
				

			case "listconns":								// generate json describing where the named host is attached (switch/port)
				if ntokens < 2 {
					nerrors++
					reason = fmt.Sprintf( "incorrect number of parameters supplied (%d) 1 expected: usage: attached2 hostname", ntokens-1 ); 
				} else {
					req = ipc.Mk_chmsg( )
					req.Send_req( nw_ch, my_ch, REQ_LISTCONNS, &tokens[1], nil )
					req = <- my_ch
					if req.State == nil {
						state = "OK"
						jreason = string( req.Response_data.(string) )
					} else {
						state = "ERROR"
						reason = fmt.Sprintf( "%s", req.State )
						jreason = ""
						nerrors++
					}
				}
				
			case "reserve":
					tmap := gizmos.Toks2map( tokens )	// allow cookie=string dscp=n bandw=in[,out] hosts=h1,h2 window=[start-]end 
					if len( tmap ) < 1  {
						if ntokens < 4  {		
							nerrors++
							reason = fmt.Sprintf( "incorrect number of parameters supplied (%d): usage: reserve <bandwidth[K|M|G][,<outbandw[K|M|G]> [<start>-]<end-time> <host1>[,<host2>] cookie dscp; received: %s", ntokens-1, recs[i] ); 
							break
						} 

						tmap["bandw"] = &tokens[1]			// less efficient, but easier to read and we don't do this enough to matter
						tmap["window"] = &tokens[2]
						tmap["hosts"] = &tokens[3]
						tmap["cookie"] = &empty_str
						dup_str := "0"
						tmap["dscp"] = &dup_str
						if ntokens > 4 {					// optional, cookie must be supplied if dscp is supplied
							tmap["cookie"] = &tokens[4]
							if ntokens > 5 {
								tmap["dscp"] = &tokens[5]
							}
						} 
					}

					if strings.Index( *tmap["bandw"], "," ) >= 0 {				// look for inputbandwidth,outputbandwidth
						subtokens := strings.Split( *tmap["bandw"], "," )
						bandw_in = clike.Atoll( subtokens[0] )
						bandw_out = clike.Atoll( subtokens[1] )
					} else {
						bandw_in = clike.Atoll( *tmap["bandw"] )				// no comma, so single value applied to each
						bandw_out = bandw_in
					}


					startt, endt = gizmos.Str2start_end( *tmap["window"] )		// split time token into start/end timestamps
					h1, h2 = gizmos.Str2host1_host2( *tmap["hosts"] )			// split host token into individual names
					dscp := 0
					if tmap["dscp"] != nil {
						dscp = clike.Atoi( *tmap["dscp"] )						// specific dscp value that should be propigated at the destination
					}

					res_name = mk_resname( )					// name used to track the reservation in the cache and given to queue setting commands for visual debugging
					res, err = gizmos.Mk_pledge( &h1, &h2, startt, endt, bandw_in, bandw_out, &res_name, tmap["cookie"], dscp )


					if res != nil {												// able to make the reservation, continue and try to find a path with bandwidth
						req = ipc.Mk_chmsg( )
						req.Send_req( nw_ch, my_ch, REQ_RESERVE, res, nil )		// send to network to verify a path
						req = <- my_ch											// get response from the network thread

						if req.Response_data != nil {
							path_list := req.Response_data.( []*gizmos.Path )			// path(s) that were found to be suitable for the reservation
							res.Set_path_list( path_list )

							req.Send_req( rmgr_ch, my_ch, REQ_ADD, res, nil )	// network OK'd it, so add it to the inventory
							req = <- my_ch										// wait for completion

							if req.State == nil {
								ckptreq := ipc.Mk_chmsg( )
								ckptreq.Send_req( rmgr_ch, nil, REQ_CHKPT, nil, nil )
								state = "OK"
								reason = fmt.Sprintf( "reservation accepted; reservation path has %d entries", len( path_list ) )
								jreason =  res.To_json()
							} else {
								nerrors++
								reason = fmt.Sprintf( "%s", req.State )
								jreason = ""
							}

						} else {
							reason = fmt.Sprintf( "reservation rejected: %s", req.State )
							nerrors++
						}
					} else {
						reason = fmt.Sprintf( "reservation rejected: %s", err )
						nerrors++
					}
					
			case "qdump":					// dumps a list of currently active queues from network and writes them out to requestor (debugging mostly)
				req = ipc.Mk_chmsg( )
				req.Send_req( nw_ch, my_ch, REQ_GEN_QMAP, time.Now().Unix(), nil )		// send to network to verify a path
				req = <- my_ch															// get response from the network thread
				state = "OK"
				m :=  req.Response_data.( []string )
				jreason = `{ "queues": [ `
				sep := ""						// local scope not to trash the global var
				for i := range m {
					jreason += fmt.Sprintf( "%s%q", sep, m[i] )
					sep = "," 
				}
				jreason += " ] }"
				reason = "active queues"

			case "verbose":									// verbose n [child-bleater]
				if ntokens > 1 {
					state = "OK"
					nv := clike.Atou( tokens[1] )
					if nv < 0 {
						nv = 0
					}
					if ntokens > 2 {
						jreason = fmt.Sprintf( "\"verbose set: %s now %d\"",  tokens[2], nv )
						switch( tokens[2] ) {
							case "osif", "ostack", "osif_mgr":
								osif_sheep.Set_level( nv )

							case "resmgr", "res_mgr":
								rm_sheep.Set_level( nv )

							case "fq", "fq_mgr", "fqmgr":
								fq_sheep.Set_level( nv )

							case "http", "http_api":
								http_sheep.Set_level( nv )

							case "net", "network":
								net_sheep.Set_level( nv )
								
							case "agent":
								am_sheep.Set_level( nv )

							case "tegu", "master":
								tegu_sheep.Set_level( nv )

							default:
								state = "ERROR"
								http_sheep.Baa( 1, "unrecognised subsystem name given with verbose level: %s", tokens[2], nv )
								jreason = fmt.Sprintf( "\"unrecognsed subsystem name given; must be one of: agent, osif, resmgr, http, fqmgr, or net" )
						}

						http_sheep.Baa( 1, "verbose level set: %s %d", tokens[2], nv )
					} else {
						jreason = fmt.Sprintf( "\"verbose set: master level to %d\"",   nv )
						http_sheep.Baa( 1, "verbose level set: master %d", nv )
						tegu_sheep.Set_level( nv )
					}
				} else {
					state = "ERROR"
					reason = fmt.Sprintf( "missing parameters on verbose command" )
				}

			default:
				nerrors++
				reason = fmt.Sprintf( "unrecognised put/post action: req %d, %s: whole req=(%s)", i, tokens[0], recs[i] )
				http_sheep.Baa( 1, "unrecognised action: %s in %s", tokens[0], recs[i] )
		}

		if jreason != "" {
			fmt.Fprintf( out, `%s{ "status": %q, "request": %d, "comment": %q, "details": %s }`, sep, state, req_count, reason, jreason )
		} else {
			fmt.Fprintf( out, `%s{ "status": %q, "request": %d, "comment": %q }`, sep, state, req_count, reason )
		}

		sep = ","		// after the first the separator is now a comma
	}

	fmt.Fprintf( out,  "]," )				// close the request output array (adding the comma here might be dodgy, but we'll assume the caller is sending one last object)

	if nerrors > 0 {
		state = "ERROR"		// must set on the off chance that last request was ok
	} 

	if req_count <= 0 {
		msg = fmt.Sprintf( "no requests found in input" )
		state = "ERROR"
	} else {
		msg = fmt.Sprintf( "%d errors processing requests", nerrors )
	}
	
	return
}

func parse_put( out http.ResponseWriter, recs []string ) (state string, msg string) {
	
	state, msg = parse_post( out, recs )
	return
}

/*
	Delete something. Currently only reservation is supported, but there might be other
	things in future to delete, so we require a token 0 that indiccates what.

	Supported delete actions:
		reservation <name> [<cookie>]
*/
func parse_delete( out http.ResponseWriter, recs []string ) (state string, msg string) {
	var (
		sep			string = ""							// json output list separator
		req_count	int = 0								// requests processed this batch
		req			*ipc.Chmsg								// also used to receive a response
		tokens		[]string								// parsed tokens from the http data
		ntokens		int
		nerrors		int = 0								// overall error count -- final status is error if non-zero
		jdetails	string = ""							// result details in json
		comment		string = ""							// comment about the state
		my_ch		chan *ipc.Chmsg
		del_data	[]*string								// data sent on delete
		res			*gizmos.Pledge								// reservation fetched from resmgr
	)

	my_ch = make( chan *ipc.Chmsg )							// allocate channel for responses to our requests
	defer close( my_ch )

	fmt.Fprintf( out,  "\"reqstate\":[ " )				// wrap request output into an array
	state = "OK"
	for i := 0; i < len( recs ); i++ {
		ntokens, tokens = token.Tokenise_qpopulated( recs[i], " " )		// split and keep populated tokens (treats successive sep chrs as one), preserves spaces in "s

		if ntokens < 1 || tokens[0][0:1] == "#" {			// skip comments or blank lines
			continue
		}

		req_count++
		state = "ERROR"
		jdetails = ""

		http_sheep.Baa( 2, "delete command for %s", tokens[0] )
		switch tokens[0] {
			case "reservation":									// expect:  reservation name(id) [cookie]
				if ntokens < 2 || ntokens > 3  {
					nerrors++
					comment = fmt.Sprintf( "bad delete reservation command: wanted 'reservation res-ID [cookie]' received '%s'", recs[i] ); 
				} else {
					del_data = make( []*string, 2, 2 )			// deletee data is the reservation name and the cookie if supplied
					del_data[0] = &tokens[1]
					if ntokens < 3 {
						del_data[1] = &empty_str

					} else {
						del_data[1] = &tokens[2]
					}

					req = ipc.Mk_chmsg( )
					//req.Send_req( rmgr_ch, my_ch, REQ_GET, del_data, nil )		// must get the reservation to del from network
					//req = <- my_ch											// wait for response

					//if req.State == nil {									// no error, response data contains the pledge
					//	res = req.Response_data.( *gizmos.Pledge )
						req.Send_req( rmgr_ch, my_ch, REQ_DEL, del_data, nil )	// delete from the resmgr point of view
						req = <- my_ch										// wait for delete response 

						if req.State == nil {								// no error deleting in res mgr
							req.Send_req( nw_ch, my_ch, REQ_DEL, res, nil )		// delete from the network point of view
							req = <- my_ch									// wait for response from network

							if req.State == nil {
								comment = "reservation successfully deleted"
								state = "OK"
							} else {
								nerrors++
								comment = fmt.Sprintf( "reservation delete failed: %s", state )
							}

						} else {
							nerrors++
							comment = fmt.Sprintf( "reservation delete failed: %s", state )
						}

					//} else {					// get fails if cookie is wrong, or if it doesn't exist
						//nerrors++
						//comment = fmt.Sprintf( "reservation delete failed: %s", req.State )
					//}
				}

			default:
				nerrors++
				comment = fmt.Sprintf( "unknown delete command: %s", tokens[0] )
				
		}

		if jdetails != "" {
			fmt.Fprintf( out, "%s{ \"status\": \"%s\", \"request\": \"%d\", \"comment\": \"%s\", \"details\": %s }", sep, state, req_count, comment, jdetails )
		} else {
			fmt.Fprintf( out, "%s{ \"status\": \"%s\", \"request\": \"%d\", \"comment\": \"%s\" }", sep, state, req_count, comment )
		}

		sep = ","
	}

	fmt.Fprintf( out,  "]," )				// close the request output array (adding the comma here might be dodgy, but we'll assume the caller is sending one last object)

	if nerrors > 0 {
		state = "ERROR"		// must set on the off chance that last request was ok
	} 

	if req_count <= 0 {
		msg = fmt.Sprintf( "no requests found in input" )
		state = "ERROR"
	} else {
		msg = fmt.Sprintf( "%d errors processing requests in %d requests", nerrors, req_count )
	}

	return
}

func parse_get( out http.ResponseWriter, recs []string ) (state string, msg string) {
	http_sheep.Baa( 1, "get received -- unsupported" )
	state = "OK"
	msg = "unsupported"
	return
}

/*
	Deal with input from the other side; this is invoked directly by the http listener.
	Because we are driven as a callback, and cannot controll the parameters passed in, we 
	must (sadly) rely on globals for some information; sigh. (There might be a way to deal
	with this using a closure, but I'm not taking the time to go down that path until 
	other more important things are implemented.)

	This function splits input, on either newlines or semicolons, into records. The array
	of records is then passed to the appropriate parse function based on the http method
	(PUT, GET, etc) that was used by the user-agent. 

	Output to the client process is a bunch of {...} "objects", one per record, 
	plus a final overall status; all are collected in square brackets and thus
	should be parsable as json.
*/
func deal_with( out http.ResponseWriter, in *http.Request ) {
	var data 	[]byte
	var	recs	[]string
	var state	string
	var msg		string

	data = dig_data( in )
	if( data == nil ) {						// missing data -- punt early
		http_sheep.Baa( 1, "http: deal_with called without data: %s", in.Method )
		//fmt.Fprintf( out, "{ \"status\": \"ERROR\", \"comment\": \"missing command\" }", in.Method )
		fmt.Fprintf( out, `{ "status": "ERROR", "comment": "missing command" }` )
		return

	} else {
		_, recs = token.Tokenise_drop( string( data ), ";\n" )		// split based on ; or newline
		fmt.Fprintf( out, "{ " )									// open the overall object for output
	}
	
	switch in.Method {
		case "PUT":
			state, msg = parse_put( out, recs )

		case "POST":
			state, msg = parse_post( out, recs )

		case "DELETE":
			state, msg = parse_delete( out, recs )

		case "GET":
			state, msg = parse_get( out, recs )

		default:
			http_sheep.Baa( 1, "deal_with called for unrecognised method: %s", in.Method )
			state = "ERROR"
			msg = fmt.Sprintf( "unrecognised method: %s", in.Method )
	}

	fmt.Fprintf( out, fmt.Sprintf( ` "endstate": { "status": %q, "comment": %q } }`, state, msg ) )		// final, overall status and close bracket

}

/*
	start an http listener. we expect channels and the port to be in globals.
*/
func Http_api( api_port *string, nwch chan *ipc.Chmsg, rmch chan *ipc.Chmsg ) {

	http_sheep = bleater.Mk_bleater( 0, os.Stderr )		// allocate our bleater and attach it to the master
	http_sheep.Set_prefix( "http_api" )
	tegu_sheep.Add_child( http_sheep )					// we become a child so that if the master vol is adjusted we'll react too

	if p := cfg_data["httpmgr"]["verbose"]; p != nil {
		http_sheep.Set_level(  uint( clike.Atoi( *p ) ) )
	}
	http_sheep.Baa( 1, "http interface running" )

	http.HandleFunc( "/tegu/api", deal_with )				// define callback 
	err := http.ListenAndServe( ":" + *api_port, nil )		// drive the bus
	
	http_sheep.Baa( 0, "http listener is done" )
	if( err != nil ) {
		http_sheep.Baa( 1, "ERR: %s", err )
	}
}
