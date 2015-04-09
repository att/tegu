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

				These requests are supported:
					POST:
						chkpt	(limited)
						graph	(limited)
						listconns
						listhosts	(limited)
						listres
						pause (limited)
						reserve
						resume (limited)
						verbose (limited)

					DELETE:
						reservation


					limited commands must be submitted from the host that Tegu is running on using the 
					IPV4 localhost address -- this assumes that only admins will have access to the 
					host and thus can issue the administrative commands.

	Date:		20 November 2013 (broken out of initial test on 2 Dec)
	Author:		E. Scott Daniels

	Mods:		05 May 2014 : Added agent manager to the verbose change list.
				13 May 2014 : Added support for exit-dscp value in reservation.
				22 May 2014 : Now forces a checkpoint after a successful reservation.
				06 Jun 2014 : Added support to listen on https rather than http
				10 Jun 2014 : Added requirement that certain admin commands be issued from localhost.
				16 Jun 2014 : Added token validation for priv requests and added listhosts and graph to 
					the set of priv commands.
				18 Jun 2014 : Corrected bug that was causing incorrect json goo when generating an error.
				20 Jun 2014 : Corrected bug that allowed a reservation between the same host (VM) name. 
				29 Jun 2014 : Changes to support user link limits.
				07 Jul 2014 : Change to drop the request to network manager on delete; reservation manager
					now sends that request to tighten up the timing between the two. 
					Added support for reservation refresh.
				17 Jul 2014 : Corrected typo in localhost validation check.
				18 Jul 2014 : Added better error messaging when unable to open a listening port.
				15 Aug 2014 : Corrected bug (201) -- refresh not giving rejection message when rejecting.
				24 Sep 2014 : Added support for ITONS traffic class demands. 
				09 Oct 2014 : Allow verbose even if network not initialised correctly.
				18 Nov 2014 : Changes to support lazy osif data fetching
				24 Nov 2014 : Corrected early return in update graph (preventing !//ipaddress from causing
					an ip2mac map to be forced out to fqmgr.
				16 Jan 2015 : Support port masks in flow-mods.
				27 Jan 2015 : Allow bandwidth specification to be decimal value (e.g. 155.2M)
				08 Apr 2015 : Corrected slice bounds error if input record was empty (e.g. '', no newline) 
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
	"syscall"
	"time"

	"codecloud.web.att.com/gopkgs/bleater"
	"codecloud.web.att.com/gopkgs/clike"
	"codecloud.web.att.com/gopkgs/token"
	"codecloud.web.att.com/gopkgs/ipc"
	"codecloud.web.att.com/gopkgs/security"

	"codecloud.web.att.com/tegu/gizmos"
)

/* ---- validation and authorisation functions ---------------------------------------------------------- */

/*
	Make a reservation name that should be unique across invocations of tegu.
*/
func mk_resname( ) ( string ) {
	r := res_nmseed
	res_nmseed++
	return fmt.Sprintf( "res%x_%05d", pid, r ); 
}

/*
	Validate the h1 and h2 strings translating the project name to a tenant ID if present. 
	The translated names are returned if _both_ are valid; error is set otherwise.
	In addition, if a port number is added to a host name it is stripped and returned.

	For IPv6 addresses, in order to be backwards compatable with the IPv4 notation of
	address:port, we'll require the syntax [address]:port if a port is to be supplied
	with an IPv6 address.

	If the resulting host names match (project/host[:port]) then we return an error
	as this isn't allowed. 
*/
func validate_hosts( h1 string, h2 string ) ( h1x string, h2x string, p1 *string, p2 *string, err error ) {
	var ht *string
	
	my_ch := make( chan *ipc.Chmsg )							// allocate channel for responses to our requests
	defer close( my_ch )									// close it on return
	p1 = &zero_string
	p2 = &zero_string

	if h1[0:1] == "!" {										// the external host needs to be h2 for flow-mod generation
		hx := h1											// so switch them if !address is first.
		h1 = h2
		h2 = hx
	}
	
	req := ipc.Mk_chmsg( )
	req.Send_req( osif_ch, my_ch, REQ_VALIDATE_HOST, &h1, nil )		// request to openstack interface to validate this token/project pair for host
	req = <- my_ch													// hard wait for response

	if req.State != nil {
		err = fmt.Errorf( "h1 validation failed: %s", req.State )
		return
	}

	ht, p1 = gizmos.Split_port( req.Response_data.( *string ) ) 	// split off :port from token/project/name where name is name or address
	h1x = *ht
	/*
	h1x = *( req.Response_data.( *string ) )
	tokens := strings.Split( h1x, ":" )
	if len( tokens ) > 1 {
		h1x = tokens[0]
		p1 = &tokens[1]
	}
	*/
	
	req = ipc.Mk_chmsg( )											// probably don't need a new one, but it should be safe
	req.Send_req( osif_ch, my_ch, REQ_VALIDATE_HOST, &h2, nil )		// request to openstack interface to validate this host
	req = <- my_ch													// hard wait for response

	if req.State != nil {
		err = fmt.Errorf( "h2 validation failed: %s", req.State )
		return
	}

	ht, p2 = gizmos.Split_port( req.Response_data.( *string ) ) 	// split off :port from token/project/name where name is name or address
	h2x = *ht
	//h2x = *( req.Response_data.( *string ) )
	if h1x == h2x {
		err = fmt.Errorf( "host names are the same" )
		return
	}

	/*
	tokens = strings.Split( h2x, ":" )
	if len( tokens ) > 1 {
		h2x = tokens[0]
		p2 = &tokens[1]
	}
	*/

	return
}


/*
	Return true if the sender string is the localhost (127.0.0.1).
*/
func is_localhost( a *string ) ( bool ) {
	tokens := strings.Split( *a, ":" )
	if tokens[0] == "127.0.0.1" {
		return true
	}

	return false
}

/*
	Given what is assumed to be an admin token, verify it. The admin ID is assumed to be the 
	ID defined as the default user in the config file. 

	Returns true if the token could be authorised. 
*/
func is_admin_token( token *string ) ( bool ) {

	my_ch := make( chan *ipc.Chmsg )							// allocate channel for responses to our requests
	defer close( my_ch )									// close it on return
	
	req := ipc.Mk_chmsg( )
	req.Send_req( osif_ch, my_ch, REQ_VALIDATE_TEGU_ADMIN, token, nil )		// verify that the token is good for the admin (default) user given in the config file
	req = <- my_ch														// hard wait for response

	if req.State == nil {
		return true
	}

	http_sheep.Baa( 1, "admin token auth failed: %s", req.State )
	return false
}

/*
	Given a token test to see if any of the roles in the list are listed as roles by openstack.
	Returns true if one or more are listed.
*/
func token_has_osroles( token *string, roles string ) ( bool ) {
	dstr := *token + " " + roles					// osif expects single string, space separated token and list
	
	my_ch := make( chan *ipc.Chmsg )						// allocate channel for responses to our requests
	defer close( my_ch )									// close it on return
	
	req := ipc.Mk_chmsg( )
	req.Send_req( osif_ch, my_ch, REQ_HAS_ANY_ROLE, &dstr, nil )		// go check it out
	req = <- my_ch														// hard wait for response

	if req.State == nil {
		return true
	}

	return false
}

/*
	This function will validate the requestor is authorised to make the request based on the setting 
	of priv_auth. When localhost, the request must have originated from the localhost. When token
	the user must have sent a valid token for the admin user defined in the config file. When none, 
	we just return true. 

	Returns true if the command can be allowed; false if not. 
*/
func validate_auth( data *string, is_token bool ) ( allowed bool ) {
	if priv_auth == nil {
		return true
	}

	switch *priv_auth {
		case "none":
			return true

		case "local":
			fallthrough
		case "localhost":
			if ! is_token {
				return is_localhost( data )
			}
			fallthrough

		case "token":
			return is_admin_token( data )
	}

	return false
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
	Given a reservation (pledge) ask network manager to reserve the bandwidth and set queues. If net mgr
	is successful, then we'll send the reservation off to reservation manager to do the rest (push flow-mods
	etc.)  The return values may seem odd, but are a result of breaking this out of the main parser which
	wants two reason strings and a count of errors in order to report an overall status and a status of
	each request that was received from the outside world. 
*/
func finalise_reservation( res *gizmos.Pledge, res_paused bool ) ( reason string, jreason string, nerrors int ) {

	nerrors = 0
	jreason = ""
	reason = ""

	my_ch := make( chan *ipc.Chmsg )						// allocate channel for responses to our requests
	defer close( my_ch )									// close it on return

	req := ipc.Mk_chmsg( )
	req.Send_req( nw_ch, my_ch, REQ_RESERVE, res, nil )		// send to network to verify a path
	req = <- my_ch											// get response from the network thread

	if req.Response_data != nil {
		path_list := req.Response_data.( []*gizmos.Path )			// path(s) that were found to be suitable for the reservation
		res.Set_path_list( path_list )

		req.Send_req( rmgr_ch, my_ch, REQ_ADD, res, nil )	// network OK'd it, so add it to the inventory
		req = <- my_ch										// wait for completion

		if req.State == nil {
			ckptreq := ipc.Mk_chmsg( )
			ckptreq.Send_req( rmgr_ch, nil, REQ_CHKPT, nil, nil )	// request a chkpt now, but don't wait on it
			reason = fmt.Sprintf( "reservation accepted; reservation path has %d entries", len( path_list ) )
			jreason =  res.To_json()
		} else {
			nerrors++
			reason = fmt.Sprintf( "%s", req.State )
		}

		if res_paused {
			rm_sheep.Baa( 1, "reservations are paused, accepted reservation will not be pushed until resumed" )
			res.Pause( false )								// when paused we must mark the reservation as paused and pushed so it doesn't push until resume received
			res.Set_pushed( )
		}
	} else {
		reason = fmt.Sprintf( "reservation rejected: %s", req.State )
		nerrors++
	}

	return
}


/*
	Gathers information about the host from openstack, and if known inserts the information into 
	the network graph. If block is true, then we will block on a repl from network manager. 
	If update_fqmgr is true, then we will also send osif a request to update the fqmgr with 
	data that might ahve changed as a result of lazy gathering of info by the get_hostinfo
	request.  If block is set, then we block until osif acks the request. This ensures
	that the request has been given to fq-mgr which is single threaded and thus will process
	the update before attempting to process any flow-mods that result from a later reservation.
*/
func update_graph( hname *string, update_fqmgr bool, block bool ) {

	my_ch := make( chan *ipc.Chmsg )							// allocate channel for responses to our requests

	req := ipc.Mk_chmsg( )
	req.Send_req( osif_ch, my_ch, REQ_GET_HOSTINFO, hname, nil )				// request data
	req = <- my_ch
	if req.Response_data != nil {												// if returned send to network for insertion
		if ! block {
			my_ch = nil															// turn off if not blocking
		}

		req.Send_req( nw_ch, my_ch, REQ_ADD, req.Response_data, nil )			// add information to the graph
		if block {
			_ = <- my_ch															// wait for response -- at the moment we ignore
		}
	} else {
		if req.State != nil {
			http_sheep.Baa( 2, "unable to get host info on %s: %s", req.State )		// this is probably ok as it's likely a !//ipaddress hostname, but we'll log it anyway
		}
	}

	if update_fqmgr {
		req := ipc.Mk_chmsg( )
		req.Send_req( osif_ch, my_ch, REQ_IP2MACMAP, hname, nil )				// cause osif to push changes into fq-mgr (caution: we give osif fq-mgr's channel for response)
		if block {
			_ = <- my_ch
		}	
	} 
}



// ---- main parsers ------------------------------------------------------------------------------------ 
/*
	parse and react to a POST request. we expect multiple, newline separated, requests
	to be sent in the body. Supported requests:

		ckpt
		listhosts
		listulcaps
		listres
		listconns
		reserve <bandwidth[K|M|G][,outbandwidth[K|M|G]> [<start>-]<end> <host1>[-<host2] [cookie]
		graph
		ping
		listconns <hostname|hostip>


	Because this is drien from within the go http support library, we expect a few globals 
	to be in our envronment to make things easier.  
		accept_requests	bool	set to true if we can accept and process requests. if false any
								request is failed.
*/
func parse_post( out http.ResponseWriter, recs []string, sender string ) (state string, msg string) {
	var (
		res			*gizmos.Pledge			// reservation that we're working on
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
		sep			string = ""				// json object separator
		req			*ipc.Chmsg
		my_ch		chan *ipc.Chmsg
		auth_data	string					// data (token or sending address) sent for authorisation
		is_token	bool					// flag when auth data is a token
		ecount		int						// number of errors reported by function
	)


	my_ch = make( chan *ipc.Chmsg )							// allocate channel for responses to our requests
	defer close( my_ch )

	fmt.Fprintf( out,  "\"reqstate\": [ " )				// wrap request output into an array

	state = "OK"
	for i := 0; i < len( recs ); i++ {
		ntokens, tokens = token.Tokenise_qpopulated( recs[i], " " )		// split and keep populated tokens (treats successive sep chrs as one), preserves spaces in "s

		if ntokens < 1 || len( tokens[0] ) < 2 || tokens[0][0:1] == "#" {		// prevent issues if empty line, skip comment.
			continue
		}

		if len( tokens[0] ) > 5  && tokens[0][0:5] == "auth="	{
			auth_data = tokens[0][5:]
			tokens = tokens[1:]				// reslice to skip the jibberish
			ntokens--
			is_token = true
		} else {
			auth_data = sender 
			is_token = false
		}

		req_count++
		state = "ERROR"				// default for each loop; final set based on error count following loop
		jreason = ""
		if accept_requests  ||  tokens[0] == "ping"  || tokens[0] == "verbose" {			// always allow ping/verbose if we are up
			reason = fmt.Sprintf( "you are not authorised to submit a %s command", tokens[0] )

			http_sheep.Baa( 3, "processing request: %s", tokens[0] )
			switch tokens[0] {

				case "chkpt":
					if validate_auth( &auth_data, is_token ) {
						req = ipc.Mk_chmsg( )
						req.Send_req( rmgr_ch, nil, REQ_CHKPT, nil, nil )
						state = "OK"
						reason = "checkpoint was requested"
					} 

				case "graph":
					if (is_token && token_has_osroles( &auth_data, "admin,sys_app" )) || validate_auth( &auth_data, is_token ) {
						req = ipc.Mk_chmsg( )
		
						req.Send_req( nw_ch, my_ch, REQ_NETGRAPH, nil, nil )	// request to net thread; it will create a json blob and attach to the request which it sends back
						req = <- my_ch											// hard wait for network thread response
						if req.Response_data != nil {
							state = "OK"
							jreason = string( req.Response_data.(string) )
							reason = ""
						} else {
							reason = "no output from network thread"
						}
					} 
		
				case "listulcaps":											// list user link capacities known to network manager
					if validate_auth( &auth_data, is_token ) {
						req = ipc.Mk_chmsg( )
						req.Send_req( nw_ch, my_ch, REQ_LISTULCAP, nil, nil )
						req = <- my_ch
						if req.State == nil {
							state = "OK"
							jreason = string( req.Response_data.(string) )
							reason = ""
						} else {
							reason = fmt.Sprintf( "%s", req.State )
						}
					} 
		
				case "listhosts":											// list known host information
					if (is_token && token_has_osroles( &auth_data, "admin,sys_app" ))  ||  validate_auth( &auth_data, is_token ) {
						req = ipc.Mk_chmsg( )
						req.Send_req( nw_ch, my_ch, REQ_LISTHOSTS, nil, nil )
						req = <- my_ch
						if req.State == nil {
							state = "OK"
							jreason = string( req.Response_data.(string) )
							reason = ""
						} else {
							reason = fmt.Sprintf( "%s", req.State )
						}
					} 
				
				case "listres":											// list reservations
					req = ipc.Mk_chmsg( )
					req.Send_req( rmgr_ch, my_ch, REQ_LIST, nil, nil )
					req = <- my_ch
					if req.State == nil {
						state = "OK"
						jreason = string( req.Response_data.(string) )
						reason = ""
					} else {
						reason = fmt.Sprintf( "%s", req.State )
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
							reason = ""
						} else {
							reason = fmt.Sprintf( "%s", req.State )
						}
					}

				case "pause":
					if validate_auth( &auth_data, is_token ) {
						if res_paused {							// already in a paused state, just say so and go on
							jreason = fmt.Sprintf( `"reservations already in a paused state; use resume to return to normal operation"` )
							state = "WARN"
						} else {
							req = ipc.Mk_chmsg( )
							req.Send_req( rmgr_ch, my_ch, REQ_PAUSE, nil, nil )
							req = <- my_ch
							if req.State == nil {
								http_sheep.Baa( 1, "reservations are now paused" )	
								state = "OK"
								jreason = string( req.Response_data.( string ) )
								reason = ""
								res_paused = true
							} else {
								reason = fmt.Sprintf( "s", req.State )
							}
						}
					} 

				case "ping":
					reason = ""
					jreason = fmt.Sprintf( "\"pong: %s\"", version )
					state = "OK"

				case "qdump":					// dumps a list of currently active queues from network and writes them out to requestor (debugging mostly)
					if validate_auth( &auth_data, is_token ) {
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
					}
					
				case "refresh":								// refresh reservations for named VM(s)
					if validate_auth( &auth_data, is_token ) {
						state = "OK"
						reason = ""
						for i := 1; i < ntokens; i++ {
							req = ipc.Mk_chmsg( )
							req.Send_req( osif_ch, my_ch, REQ_XLATE_HOST, &tokens[i], nil )		// translate [token/][project/]host-name into ID/hostname
							req = <- my_ch														// wait for response
							if req.Response_data != nil {
								hname := req.Response_data.( *string )
								req.Send_req( rmgr_ch, my_ch, REQ_PLEDGE_LIST, hname, nil )		// get a list of pledges that are associated with the hostname
								req = <- my_ch
								if req.Response_data != nil {
									plist := req.Response_data.( []*gizmos.Pledge )
									http_sheep.Baa( 1, "refreshing reservations for %s, %d pledge(s)", *hname, len( plist ) )

									for i := range plist {
										req.Send_req( rmgr_ch, my_ch, REQ_YANK_RES, plist[i].Get_id(), nil )		// yank the reservation for this pledge
										req = <- my_ch

										if req.State == nil {	
											h1, h2 := plist[i].Get_hosts( ) 							// get the pldege hosts so we can update the graph
											update_graph( h1, false, false )						// pull all of the VM information from osif then send to netmgr
											update_graph( h2, true, true )							// this call will block until netmgr has updated the graph and osif has pushed updates into fqmgr

											plist[i].Reset_pushed()													// it's not pushed at this point
											reason, jreason, ecount = finalise_reservation( plist[i], res_paused )	// allocate in network and add to res manager inventory
											if ecount == 0 {
												http_sheep.Baa( 1, "reservation refreshed: %s", *plist[i].Get_id() )
											} else {
												http_sheep.Baa( 1, "unable to finalise refresh for pledge: %s", reason )
												state = "ERROR"
												nerrors += ecount - 1
											}
										} else {
											http_sheep.Baa( 1, "unable to yank reservation: %s", req.State )
										}
									}
								} else {
									http_sheep.Baa( 1, "refreshing reservations for %s, no pledges", tokens[i] )
								}
							}
						}
					}

				case "reserve":
						key_list := "bandw window hosts cookie dscp" 
						tmap := gizmos.Mixtoks2map( tokens[1:], key_list )		// map tokens in order key list names allowing key=value pairs to precede them and define optional things
						ok, mlist := gizmos.Map_has_all( tmap, key_list )		// check to ensure all expected parms were supplied
						if !ok {
							nerrors++
							reason = fmt.Sprintf( "missing parameters: (%s); usage: reserve <bandwidth[K|M|G][,<outbandw[K|M|G]> {[<start>-]<end-time>|+sec} <host1>[,<host2>] cookie dscp; received: %s", mlist, recs[i] ); 
							break
						} 

						if strings.Index( *tmap["bandw"], "," ) >= 0 {				// look for inputbandwidth,outputbandwidth
							subtokens := strings.Split( *tmap["bandw"], "," )
							bandw_in = int64( clike.Atof( subtokens[0] ) )
							bandw_out = int64( clike.Atof( subtokens[1] ) )
						} else {
							bandw_in = int64( clike.Atof( *tmap["bandw"] ) )		// no comma, so single value applied to each
							bandw_out = bandw_in
						}


						startt, endt = gizmos.Str2start_end( *tmap["window"] )		// split time token into start/end timestamps
						h1, h2 := gizmos.Str2host1_host2( *tmap["hosts"] )			// split h1-h2 or h1,h2 into separate strings

						res = nil 
						h1, h2, p1, p2, err := validate_hosts( h1, h2 )				// translate project/host[port] into tenantID/host and if token/project/name rquired validates token.

						if err == nil {
							update_graph( &h1, false, false )						// pull all of the VM information from osif then send to netmgr
							update_graph( &h2, true, true )							// this call will block until netmgr has updated the graph and osif has pushed updates into fqmgr

							dscp := tclass2dscp["voice"]							// default to using voice traffic class
							dscp_koe := false										// we do not keep it as the packet exits the environment

							if tmap["dscp"] != nil && *tmap["dscp"] != "0" {				// 0 is the old default from tegu_req (back compat)
								if strings.HasPrefix( *tmap["dscp"], "global_" ) {
									dscp_koe = true											// global_* causes the value to be retained when packets exit the environment
									dscp = tclass2dscp[(*tmap["dscp"])[7:] ]				// pull the value based on the trailing string
								} else {
									dscp = tclass2dscp[*tmap["dscp"]]
								}
								if dscp <= 0 {
									err = fmt.Errorf( "traffic classifcation string is not valid: %s", *tmap["dscp"] )
								} 
							}
		
							if err == nil {
								res_name = mk_resname( )					// name used to track the reservation in the cache and given to queue setting commands for visual debugging
								res, err = gizmos.Mk_pledge( &h1, &h2, p1, p2, startt, endt, bandw_in, bandw_out, &res_name, tmap["cookie"], dscp, dscp_koe )
							}
						}

						if res != nil {															// able to make the reservation, continue and try to find a path with bandwidth
							if tmap["ipv6"] != nil {
								res.Set_matchv6( *tmap["ipv6"] == "true" )
							}
							
							reason, jreason, ecount = finalise_reservation( res, res_paused )	// allocate in network and add to res manager inventory
							if ecount == 0 {
								state = "OK"
							} else {
								nerrors += ecount - 1 												// number of errors added to the pile by the call
							}
						} else {
							if err == nil {
								err = fmt.Errorf( "specific reason unknown" )						// ensure we have something for message
							}
							reason = fmt.Sprintf( "reservation rejected: %s", err )
						}

				case "resume":
					if validate_auth( &auth_data, is_token ) {
						if ! res_paused {							// not in a paused state, just say so and go on
							jreason = fmt.Sprintf( `"reservation processing already in a normal state"` )
							state = "WARN"
						} else {
							req = ipc.Mk_chmsg( )
							req.Send_req( rmgr_ch, my_ch, REQ_RESUME, nil, nil )
							req = <- my_ch
							if req.State == nil {
								http_sheep.Baa( 1, "reservations are now resumed" )	
								state = "OK"
								jreason = string( req.Response_data.( string ) )
								reason = ""
								res_paused = false
							} else {
								reason = fmt.Sprintf( "s", req.State )
							}
						}
					}

				case "setulcap":									// set a user link cap; expect user-name limit
					if validate_auth( &auth_data, is_token ) {
						if ntokens == 3 {
							req = ipc.Mk_chmsg( )
							req.Send_req( osif_ch, my_ch, REQ_PNAME2ID, &tokens[1], nil )		// translate the name to virtulisation assigned ID
							req = <- my_ch

							pdata := make( []*string, 2 )
							if req.Response_data != nil {					// good *string came back
								pdata[0] = req.Response_data.( *string )
								pdata[1] = &tokens[2]

								reason = fmt.Sprintf( "user link cap set for %s (%s): %s", tokens[1], *pdata[0], tokens[2] )
								req.Send_req( rmgr_ch, nil, REQ_SETULCAP, pdata, nil ) 				// dont wait for a reply
								state = "OK"
							} else {
								reason = fmt.Sprintf( "unable to translate name: %s", tokens[1] )
								state = "ERROR"
								nerrors++
							}
						} else {
							state = "ERROR"
							nerrors++
							reason = fmt.Sprintf( "incorrect number of parameters received (%d); expected tenant-name limit", ntokens )
						}
					}

				case "verbose":									// verbose n [child-bleater]
					if validate_auth( &auth_data, is_token ) {
						if ntokens > 1 {
							state = "OK"
							reason = ""
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

									case "lib", "gizmos":
										gizmos.Set_bleat_level( nv )

									default:
										state = "ERROR"
										http_sheep.Baa( 1, "unrecognised subsystem name given with verbose level: %s", tokens[2], nv )
										jreason = fmt.Sprintf( `"unrecognsed subsystem name given; must be one of: agent, osif, resmgr, http, fqmgr, or net"` )
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
					}

				default:
					reason = fmt.Sprintf( "unrecognised put and/or post action: reqest %d, %s: whole req=(%s)", i, tokens[0], recs[i] )
					http_sheep.Baa( 1, "unrecognised action: %s in %s", tokens[0], recs[i] )
			}
		} else {
			reason = fmt.Sprintf( "tegu is running, but is not accepting requests; try again later" )
		}

		if state == "ERROR" {
			nerrors++
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

func parse_put( out http.ResponseWriter, recs []string, sender string ) (state string, msg string) {
	
	state, msg = parse_post( out, recs, sender )
	return
}

/*
	Delete something. Currently only reservation is supported, but there might be other
	things in future to delete, so we require a token 0 that indiccates what.

	Supported delete actions:
		reservation <name> [<cookie>]
*/
func parse_delete( out http.ResponseWriter, recs []string, sender string ) ( state string, msg string ) {
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
					del_data = make( []*string, 2, 2 )			// delete data is the reservation name and the cookie if supplied
					del_data[0] = &tokens[1]
					if ntokens < 3 {
						del_data[1] = &empty_str

					} else {
						del_data[1] = &tokens[2]
					}

					req = ipc.Mk_chmsg( )
					req.Send_req( rmgr_ch, my_ch, REQ_DEL, del_data, nil )	// delete from the resmgr point of view		// res mgr sends delete on to network mgr (2014.07.07)
					req = <- my_ch										// wait for delete response 

					if req.State == nil {
						comment = "reservation successfully deleted"
						state = "OK"
					} else {
						nerrors++
						comment = fmt.Sprintf( "reservation delete failed: %s", req.State )
					}
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

func parse_get( out http.ResponseWriter, recs []string, sender string ) (state string, msg string) {
	http_sheep.Baa( 1, "get received and ignored -- GET is not supported" )
	state = "ERROR"
	msg = "GET requests are unsupported"
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
	var (
		data 	[]byte
		recs	[]string
		state	string
		msg		string
	)

	data = dig_data( in )
	if( data == nil ) {						// missing data -- punt early
		http_sheep.Baa( 1, "http: deal_with called without data: %s", in.Method )
		fmt.Fprintf( out, `{ "status": "ERROR", "comment": "missing command" }` )	// error stuff back to user
		return
	} else {
		_, recs = token.Tokenise_drop( string( data ), ";\n" )		// split based on ; or newline
		fmt.Fprintf( out, "{ " )									// open the overall object for output
	}
	
	switch in.Method {
		case "PUT":
			state, msg = parse_put( out, recs, in.RemoteAddr )

		case "POST":
			state, msg = parse_post( out, recs, in.RemoteAddr )

		case "DELETE":
			state, msg = parse_delete( out, recs, in.RemoteAddr )

		case "GET":
			state, msg = parse_get( out, recs, in.RemoteAddr )

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
	var (
		ssl_key	*string = nil
		ssl_cert *string = nil
		create_cert bool = false
		err	error
	)

	http_sheep = bleater.Mk_bleater( 0, os.Stderr )		// allocate our bleater and attach it to the master
	http_sheep.Set_prefix( "http_api" )
	tegu_sheep.Add_child( http_sheep )					// we become a child so that if the master vol is adjusted we'll react too

	dup_str := "localhost"
	priv_auth = &dup_str
	
	tclass2dscp = make( map[string]int, 5 )			// TODO: these need to come from the config file
	tclass2dscp["voice"] = 46
	tclass2dscp["control"] = 26
	tclass2dscp["data"] = 18

	if cfg_data["httpmgr"] != nil {
		if p := cfg_data["httpmgr"]["verbose"]; p != nil {
			http_sheep.Set_level(  uint( clike.Atoi( *p ) ) )
		}

		p := cfg_data["httpmgr"]["cert"]
		if p != nil {
			ssl_cert = p
		}

		p = cfg_data["httpmgr"]["key"]
		if p != nil {
			ssl_key = p
		}

		p = cfg_data["httpmgr"]["create_cert"]
		if p != nil  && *p == "true" {	
			create_cert = true
		}
		
		p = cfg_data["httpmgr"]["priv_auth"]
		if p != nil {
			switch *p {
				case "none":
					priv_auth = p

				case "local":
					priv_auth = p

				case "localhost":
					priv_auth = p

				case "token":
					priv_auth = p

				default:
					http_sheep.Baa( 0, `WRN: invalid local authorisation type (%s), defaulting to "localhost"  [TGUHTP000]`, *p )
			}
		}
	}

	http.HandleFunc( "/tegu/api", deal_with )				// define callback 
	if ssl_cert != nil && *ssl_cert != "" && ssl_key != nil && *ssl_key != "" {
		if  create_cert {
			http_sheep.Baa( 1, "creating SSL certificate and key: %s %s", *ssl_cert, *ssl_key )
			dns_list := make( []string, 3 )
			dns_list[0] = "localhost"
			this_host, _ := os.Hostname( )
			tokens := strings.Split( this_host, "." )
			dns_list[1] = this_host
			dns_list[2] = tokens[0]	
			cert_name := "tegu_cert"
			err = security.Mk_cert( 1024, &cert_name, dns_list, ssl_cert, ssl_key )
    		if err != nil {
				http_sheep.Baa( 0, "ERR: unable to create a certificate: %s %s: %s  [TGUHTP001]", ssl_cert, ssl_key, err )
			}
		}

		http_sheep.Baa( 1, "http interface running and listening for TLS connections on %s", *api_port )
		err = http.ListenAndServeTLS( ":" + *api_port, *ssl_cert, *ssl_key,  nil )		// drive the bus
	} else {
		http_sheep.Baa( 1, "http interface running and listening for connections on %s", *api_port )
		err = http.ListenAndServe( ":" + *api_port, nil )		// drive the bus
	}
	
	if err != nil {
		http_sheep.Baa( 1, "ERR: unable to start http listener: %s  [TGUHTP002]", err )
		syscall.Exit( 1 )								// bring the giant down hard if we cannot listen
	} else {
		http_sheep.Baa( 0, "http listener is done" )
	}
}
