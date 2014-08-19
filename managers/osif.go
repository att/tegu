// vi: sw=4 ts=4:

/*

	Mnemonic:	osif -- openstack interface manager
	Abstract:	Manages the interface to all defined openstack environments.
				The main function here should be executed as a goroutine and will
				set up various ticklers to drive things like the rebuilding of
				the vm2ip map. Other components may send requests to the osif_mgr
				as needed.

				Maps built:
					openstack, VMs (ID and name) to IP address

				The trick with openstack is that there may be more than one project
				(tenant) that we need to find VMs in.  We will depend on the config
				file data (global) which should contain a list of each openstack section 
				defined in the config, and for each section we expect it to contain:

					url 	the url for the authorisation e.g. "http://135.197.225.209:5000/"
    				usr 	the user name that can be authorised with passwd
    				passwd	the password
    				project	the project/tenant name

				For each section an openstack object is created and for each openstack
				related translation that is needed all objects will be used to query
				openstack.   At present there is no means to deal with information
				that might be duplicated between openstack projects. (This might become
				an issue if user defined networking selects the same address range(s).

	Date:		28 December 2013
	Author:		E. Scott Daniels

	Mods:		05 May 2014 - Changes to support digging the various maps out of openstack
					that are needed when we are not using floodlight.
				19 May 2014 - Changes to support floating ip translation map generation.
				05 Jun 2014 - Added support for pulling all tenants rather than just those
					listed with credientials and building project to ID map.
				07 Jun 2014 - Added function to validate hosts if supplied with token and 
					to translate project (tenant) name into an ID. 
				09 Jun 2014 - Converted the openstack cred list to a map.
				10 Jun 2014 - Changes to ignore the "_ref_" entry in the cred map. 
				21 Jun 2014 - Clarification in comment. 
				29 Jun 2014 - Changes to support user link limits.
				06 Jul 2014 - Changes to support refresh reservations.
				15 Jul 2014 - Added support for dash (-) as a token which skips authorisation
					but marks the resulting ID as unauthorised with a leading dash.
				16 Jul 2014 - Changed unvalidated indicator to bang (!) to avoid issues when 
					vm names have a dash (gak).
				14 Aug 2014 - Corrected comment.
				15 Aug 2014 - Changed pointer reference on calls to ostk for clarity (was os).
				19 Aug 2014 - Fix for bug #202 -- need to return nil if project ID not known.
*/

package managers

import (
	//"bufio"
	//"errors"
	"fmt"
	//"io"
	"os"
	"strings"
	//"time"

	"forge.research.att.com/gopkgs/bleater"
	"forge.research.att.com/gopkgs/clike"
	"forge.research.att.com/gopkgs/ipc"
	"forge.research.att.com/gopkgs/ostack"
	"forge.research.att.com/gopkgs/token"
	//"forge.research.att.com/tegu/gizmos"
)

//var (
// NO GLOBALS HERE; use globals.go because of the shared namespace
//)


// --- Private --------------------------------------------------------------------------

/*
	Given a raw string of the form [[<token>]/{project-name|ID}]/<data> verify
	that token is valid for project, and translate project to an ID.  The resulting output
	is a string tenant_id/<data> (token is stripped) if the token was valid for the project. 
	If the token was not valid, then the resulting string is nil and error will be set. 

	If token is omitted from the raw string, and is not required, the project name is 
	translated to a tenant ID in the resulting string (if supplied). If the token is reqired, 
	the input is considered invalid if it is missing and nil is returned with an appropriate
	eror message in error.

	If tok_req is true, then the raw string passed in _must_ contain a valid token and 
	is considered invalid if it does not.

	Yes, we could loop through os_list assuming we're looking for a project name, but
	it's cleaner to maintain a hash. 
*/
func validate_token( raw *string, os_refs map[string]*ostack.Ostack, pname2id map[string]*string, tok_req bool ) ( *string, error ) {
	var (
		id	string
		idp	*string = nil
		err	error
	)

	err = fmt.Errorf( "token prefixed host names are required (token/tenant/hostname): token not found" )		// generic error if we need a token and one not supplied

	tokens := strings.SplitN( *raw, "/", 3 )
	switch( len( tokens ) ) {
		case 1:							// assume hostname only
			if tok_req {
				return nil, err
			} else {
				return raw, nil
			}
		
		case 2:							// assume just project/hostname; translate project
			if tok_req {
				return nil, err
			} else {
				if pname2id != nil {
					idp = pname2id[tokens[0]]
				}
				if idp == nil {				// assume its an ID and go with it
					return raw, nil
				}
				xstr := fmt.Sprintf( "%s/%s", *idp, tokens[1] )	// build and return the translated string
				return &xstr, nil
			}

		case 3:							// token and name/ID
			if pname2id != nil {
				idp = pname2id[tokens[1]]
			}
			if idp == nil {					// assume it's already an id and needs no translation
				id = tokens[1]
			} else {
				id = *idp
			}

			if ! tok_req {										// using this for translation, skip the osif call
				xstr := fmt.Sprintf( "%s/%s", id, tokens[2] )	// build and return the translated string
				return &xstr, nil
			}

			if tokens[0] == "!"	{								// special indication to skip validation and return ID with a lead bang indicating not authorised
				xstr := fmt.Sprintf( "!%s/%s", id, tokens[2] )	// build and return the translated string
				return &xstr, nil
			}

			for _, ostk := range os_refs {										// find the project name in our list
				if ostk != nil  &&  ostk.Equals_id( &id ) {
					ok, err := ostk.Valid_for_project( &(tokens[0]), false ) 		// verify that token is legit for the project
					if ok {
						xstr := fmt.Sprintf( "%s/%s", id, tokens[2] )			// build and return the translated string
						return &xstr, nil
					} else {
						osif_sheep.Baa( 1, "invalid token: %s: %s", *raw, err )
					}

					break			// bail out and exit
				}
			}
	}
	
	return nil, fmt.Errorf( "invalid token/tenant pair" )
}


/*
	Verifies that the token passed in is a valid token for the default user given in the 
	config file. 
	Returns "ok" if it is good, and an error otherwise. 	
*/
func validate_admin_token( admin *ostack.Ostack, token *string, user *string ) ( error ) {

osif_sheep.Baa( 1, "validating admin token" )
	err := admin.Token_validation( token, user ) 		// ensure token is good and was issued for user
	if err == nil {
osif_sheep.Baa( 1, "admin token validated successfully: %s", *token )
	} else {
osif_sheep.Baa( 1, "admin token invalid: %s", err )
}

	return err
}

func mapvm2ip( admin *ostack.Ostack, os_refs map[string]*ostack.Ostack ) ( m  map[string]*string ) {
	var (
		err	error
	)
	
	m = nil
	for k, ostk := range os_refs {
		if k != "_ref_" {	
			osif_sheep.Baa( 1, "mapping VMs from: %s", ostk.To_str( ) )
			m, err = ostk.Mk_vm2ip( m )
			if err != nil {
				osif_sheep.Baa( 1, "WRN: mapvm2ip: openstack query failed: %s", err )
			}
		}
	}
	
	return
}

/*
	Returns a list of openstack compute and network hosts. Hosts where OVS is likely 
	running. 
*/
func get_hosts( os_refs map[string]*ostack.Ostack ) ( s *string, err error ) {
	var (
		ts 		string = ""
		list	*string			// list of host from the openstack world
	)

	s = nil
	err = nil
	sep := ""

	if os_refs == nil || len( os_refs ) <= 0 {
		err = fmt.Errorf( "no openstack hosts in list to query" )
		return
	}

	for k, ostk := range os_refs {
		if k != "_ref_" {
			list, err = ostk.List_hosts( ostack.COMPUTE | ostack.NETWORK )	
			if err != nil {
				osif_sheep.Baa( 0, "WRN: error accessing host list: for %s: %s", ostk.To_str(), err )
				return							// drop out on first error with no list
			} else {
				if *list != "" {
					ts += sep + *list
					sep = " "
				} else {
					osif_sheep.Baa( 1, "WRN: list of hosts not returned by %s", ostk.To_str() )	
				}
			}
		}
	}

	cmap := token.Tokenise_count( ts, " " )		// break the string, and then dedup the values
	ts = ""
	sep = ""
	for k, v := range( cmap ) {
		if v > 0 {
			ts += sep + k
			sep = " "
		}
	}

	s = &ts
	return
}

/*
	Tegu-lite
	Build all vm translation maps -- requires two actual calls out to openstack
*/
func map_all( os_refs map[string]*ostack.Ostack, inc_tenant bool  ) (
			vmid2ip map[string]*string,
			ip2vmid map[string]*string,
			vm2ip map[string]*string,
			vmid2host map[string]*string,
			ip2mac map[string]*string,
			gwmap map[string]*string,
			ip2fip map[string]*string,
			fip2ip map[string]*string,
			rerr error ) {
	
	var (
		err error
	)

	vmid2ip = nil				// shouldn't need, but safety never hurts
	ip2vmid = nil
	vm2ip = nil
	vmid2host = nil
	ip2mac = nil
	gwmap = nil				// mac2ip for all gateway "boxes"
	fip2ip = nil
	ip2fip = nil

	for k, ostk := range os_refs {
		if k != "_ref_" {
			osif_sheep.Baa( 2, "creating VM maps from: %s", ostk.To_str( ) )
			vmid2ip, ip2vmid, vm2ip, vmid2host, err = ostk.Mk_vm_maps( vmid2ip, ip2vmid, vm2ip, vmid2host, inc_tenant )
			if err != nil {
				osif_sheep.Baa( 1, "WRN: unable to map VM info: %s; %s", ostk.To_str( ), err )
				rerr = err
			}
	
			ip2fip, fip2ip, err = ostk.Mk_fip_maps( ip2fip, fip2ip, inc_tenant )
			if err != nil {
				osif_sheep.Baa( 1, "WRN: unable to map VM info: %s; %s", ostk.To_str( ), err )
				rerr = err
			}
		}
	}

	// ---- use the reference pointer for these as we get everything from one call
	ip2mac, _, err = os_refs["_ref_"].Mk_mac_maps( nil, nil, inc_tenant )	
	if err != nil {
		osif_sheep.Baa( 1, "WRN: unable to map MAC info: %s; %s", os_refs["_ref_"].To_str( ), err )
		rerr = err
	}

	gwmap, _, err = os_refs["_ref_"].Mk_gwmaps( gwmap, nil, inc_tenant, false )		// second true is use project which we need right now
	if err != nil {
		osif_sheep.Baa( 1, "WRN: unable to map gateway info: %s; %s", os_refs["_ref_"].To_str( ), err )
	}

	return
}

/*
	Generate a map containing the translation from IP address to MAC address. 
*/
func get_ip2mac( os_refs map[string]*ostack.Ostack, inc_tenant bool ) ( m map[string]*string, err error ) {
	ostk := os_refs["_ref_"]
	if ostk != nil {
		m, _, err = ostk.Mk_mac_maps( nil, nil, inc_tenant )	
		if err != nil {
			osif_sheep.Baa( 1, "WRN: unable to map MAC info: %s; %s", os_refs["_ref_"].To_str( ), err )
		}
	}

	return
}

/*
	Gets an openstack interface object for the admin user.
*/
func get_admin_creds( url *string, usr *string, passwd *string, project *string ) ( creds *ostack.Ostack ) {
	creds = nil
	if url == nil || usr == nil || passwd == nil {
		return
	}

	creds = ostack.Mk_ostack( url, usr, passwd, project )		// project isn't known or needed for this

	return
}

/*
	Build a set of openstack objects for each project (tenant) that we have access to.
	Retuns the list of creds and both ID->project and project->ID maps

	We build a new map each time, copying existing references, so that if a parallel thread
	has a copy and is working from it a change to the map isn't disruptive.

	This function also sets a reference ("_ref_") entry in the map which can be used to pull
	an entry out when any of them will do. 
*/
func refresh_creds( admin *ostack.Ostack, old_list map[string]*ostack.Ostack, id2pname map[string]*string ) ( creds map[string]*ostack.Ostack, err error ) {
	var (
		r	*ostack.Ostack
	)

	creds = make( map[string]*ostack.Ostack )			// new map to fill in
	if old_list == nil {
		old_list = creds
	}

	r = nil
	for k, v := range id2pname {
		if old_list[*v] == nil  {	
			osif_sheep.Baa( 1, "adding creds for: %s/%s", k, *v )
			creds[*v], err = admin.Dup( v )				// duplicate creds for this project and then authorise to get a token
	
			if err != nil {
				osif_sheep.Baa( 1, "WRN: unable to authorise credentials for project: %s", *v )
				delete( creds, *v )
				//creds[*v] = nil
			}
		} else {
			creds[*v] = old_list[*v]					// reuse the data
			osif_sheep.Baa( 2, "reusing credentials for: %s", *v )
		}

		r = creds[*v]
	}

	creds["_ref_"] = r				// set the reference entry

	return
}

/*
	generate maps and send them to network manager.  This runs as a go routine so that 
	it doesn't block the main event processing.  It blocks waiting for an updated list
	of openstack credentials, so it will run each time the credentials are updated. 
*/
func gen_maps( data_ch chan *map[string]*ostack.Ostack, inc_tenant bool  ) {

	err_count := 0
	osif_sheep.Baa( 1, "gen_maps sub-go is running" )

	for {
			os_refs :=<- data_ch
				vmid2ip, ip2vmid, vm2ip, vmid2host, ip2mac, gwmap, ip2fip, fip2ip, _ := map_all( *os_refs, inc_tenant )
				// ignore errors as a bad entry for one ostack credential shouldn't spoil the rest of the info gathering; we send only non-nil maps
				if vm2ip != nil {
					msg := ipc.Mk_chmsg( )
					msg.Send_req( nw_ch, nil, REQ_VM2IP, vm2ip, nil )					// send w/o expecting anything back
				} else {
					err_count++
				}
	
				if vmid2ip != nil {
					msg := ipc.Mk_chmsg( )
					msg.Send_req( nw_ch, nil, REQ_VMID2IP, vmid2ip, nil )					
				} else {
					err_count++
				}
	
				if ip2vmid != nil {
					msg := ipc.Mk_chmsg( )
					msg.Send_req( nw_ch, nil, REQ_IP2VMID, ip2vmid, nil )				
				} else {
					err_count++
				}
	
				if vmid2host != nil {
					msg := ipc.Mk_chmsg( )
					msg.Send_req( nw_ch, nil, REQ_VMID2PHOST, vmid2host, nil )		
				} else {
					err_count++
				}
	
				if ip2mac != nil {
					msg := ipc.Mk_chmsg( )
					msg.Send_req( nw_ch, nil, REQ_IP2MAC, ip2mac, nil )		
				} else {
					err_count++
				}
	
				if gwmap != nil {
					msg := ipc.Mk_chmsg( )
					msg.Send_req( nw_ch, nil, REQ_GWMAP, gwmap, nil )		
				} else {
					err_count++
				}

				if ip2fip != nil {
					msg := ipc.Mk_chmsg( )
					msg.Send_req( nw_ch, nil, REQ_IP2FIP, ip2fip, nil )		
				} else {
					err_count++
				}

				if fip2ip != nil {
					msg := ipc.Mk_chmsg( )
					msg.Send_req( nw_ch, nil, REQ_FIP2IP, fip2ip, nil )		
				} else {
					osif_sheep.Baa( 1, "nil map not sent to network: fip2ip" )
					err_count++
				}

				osif_sheep.Baa( 1, "gen_maps: VM maps were updated from openstack %d issues detected", err_count )
	}
}

// --- Public ---------------------------------------------------------------------------


/*
	executed as a goroutine this loops waiting for messages from the tickler and takes
	action based on what is needed.
*/
func Osif_mgr( my_chan chan *ipc.Chmsg ) {

	var (
		msg	*ipc.Chmsg
		os_list string = ""
		os_sects	[]string					// sections in the config file
		os_refs		map[string]*ostack.Ostack			// creds for each project we need to request info from
		os_admin	*ostack.Ostack				// admin creds
		inc_tenant	bool = false
		refresh_delay	int = 15				// config file can override
		id2pname		map[string]*string			// project id/name translation maps
		pname2id	map[string]*string
		req_token	bool = false				// if set to true in config file the token _must_ be present when called to validate
		def_passwd	*string						// defaults and what we assume are the admin creds
		def_usr		*string
		def_url		*string
		def_project	*string
	)

	osif_sheep = bleater.Mk_bleater( 0, os.Stderr )		// allocate our bleater and attach it to the master
	osif_sheep.Set_prefix( "osif_mgr" )
	tegu_sheep.Add_child( osif_sheep )					// we become a child so that if the master vol is adjusted we'll react too


	// ---- pick up configuration file things of interest --------------------------

	if cfg_data["osif"] != nil {								// cannot imagine that this section is missing, but don't fail if it is
		p := cfg_data["osif"]["include_tenant"] 
		if p != nil {
			if *p == "true" {
				inc_tenant = true							// see require token option below 
			}
		}
		def_passwd = cfg_data["osif"]["passwd"]				// defaults applied if non-section given in list, or info omitted from the section
		def_usr = cfg_data["osif"]["usr"]
		def_url = cfg_data["osif"]["url"]
		def_project = cfg_data["osif"]["project"]
	
		p = cfg_data["osif"]["refresh"] 
		if p != nil {
			refresh_delay = clike.Atoi( *p ); 			
			if refresh_delay < 15 {
				osif_sheep.Baa( 1, "resresh was too small (%ds), setting to 30s", refresh_delay )
				refresh_delay = 30
			}
		}
	
		p = cfg_data["osif"]["ostack_list"] 				// preferred placement in osif section
		if p == nil {
			p = cfg_data["default"]["ostack_list"] 			// originally in default, so backwards compatable
		}
		if p != nil {
			os_list = *p
		}

		p = cfg_data["osif"]["require_token"]
		if p != nil && *p == "true"	{
			req_token = true
			inc_tenant = true					// implied if token is required
		}
	}

	gen_maps_ch := make( chan *map[string]*ostack.Ostack, 10 )					// channel to send gen_maps data to work on
	go gen_maps( gen_maps_ch,  inc_tenant )

	if os_list == " " || os_list == "" || os_list == "off" {
		osif_sheep.Baa( 0, "WRN: osif disabled: no openstack list (ostack_list) defined in configuration file or setting is 'off'" )
	} else {
		// TODO -- investigate getting id2pname maps from each specific set of creds defined if an overarching admin name is not given

		os_admin = get_admin_creds( def_url, def_usr, def_passwd, def_project )
		if os_admin != nil {
			pname2id, id2pname, _ = os_admin.Map_tenants( )		// get the translation maps
			for k := range pname2id {
				osif_sheep.Baa( 1, "tenant known: %s", k )
			}
		} else {
			id2pname = make( map[string]*string )				// empty maps and we'll never generate a translation from project name to tenant ID since there are no default admin creds
			pname2id = make( map[string]*string )
			if def_project != nil {
				osif_sheep.Baa( 0, "WRN: unable to use admin information (%s, %s) to authorise with openstack", def_usr, def_project )
			} else {
				osif_sheep.Baa( 0, "WRN: unable to use admin information (%s, no-project) to authorise with openstack", def_usr )
			}
		}

		if os_list == "all" {
			os_refs, _ = refresh_creds( os_admin, os_refs, id2pname )		// get a list of all projects and build creds for each
			for k := range os_refs {
				osif_sheep.Baa( 1, "inital os_list member: %s", k )
			}
		} else {
			if strings.Index( os_list, "," ) > 0 {
				os_sects = strings.Split( os_list, "," )
			} else {
				os_sects = strings.Split( os_list, " " )
			}
		
			os_refs = make( map[string]*ostack.Ostack, len( os_sects ) * 2 )		// length is a guideline, not a hard value
			for i := 0; i < len( os_sects ); i++ {
				osif_sheep.Baa( 1, "creating openstack interface for %s", os_sects[i] )
				url := def_url
				usr := def_usr
				passwd := def_passwd
				project := &os_sects[i]

				if cfg_data[os_sects[i]] != nil {						// section name supplied, override defaults with information from the section
					if cfg_data[os_sects[i]]["url"] != nil {
						url = cfg_data[os_sects[i]]["url"]
					}
					if cfg_data[os_sects[i]]["usr"] != nil {
						usr = cfg_data[os_sects[i]]["usr"]
					}
					if cfg_data[os_sects[i]]["passwd"] != nil {
						passwd = cfg_data[os_sects[i]]["passwd"]
					}
					if cfg_data[os_sects[i]]["project"] != nil {
						project = cfg_data[os_sects[i]]["project"]
					}
				}
				os_refs[*project] = ostack.Mk_ostack( url, usr, passwd, project )
				os_refs["_ref_"] = os_refs[*project]					// a quick access reference when any one will do
			}
		}
	}
	// ---------------- end config parsing ----------------------------------------


	tklr.Add_spot( 3, my_chan, REQ_GENMAPS, nil, 1 )						// add tickle spot to drive us once in 3s and then another to drive us based on config refresh rate
	tklr.Add_spot( int64( refresh_delay ), my_chan, REQ_GENMAPS, nil, ipc.FOREVER );  	
	tklr.Add_spot( 3, my_chan, REQ_GENCREDS, nil, 1 )						// add tickle spot to drive us once in 3s and then another to drive us based on config refresh rate
	tklr.Add_spot( int64( 180 ), my_chan, REQ_GENCREDS, nil, ipc.FOREVER );  	

	osif_sheep.Baa( 2, "osif manager is running  %x", my_chan )
	for {
		msg = <- my_chan					// wait for next message from tickler
		msg.State = nil						// default to all OK

		osif_sheep.Baa( 3, "processing request: %d", msg.Msg_type )
		switch msg.Msg_type {
			case REQ_GENMAPS:								// driven by tickler
					if len( gen_maps_ch ) < 1  {				// only push if something isn't already queued so we don't get a huge backlog if ostack is slow
						gen_maps_ch <- &os_refs				// causes another round of maps to be generated and sent to network manager
					}

			case REQ_GENCREDS:								// driven by tickler
				if os_admin != nil {
					new_name2id, new_id2pname, err := os_admin.Map_tenants( )		// fetch new maps, overwrite only if no errors
					if err == nil {
						pname2id = new_name2id
						id2pname = new_id2pname
					} else {
						osif_sheep.Baa( 1, "WRN: unable to get tenant name/ID translation data: %s", err )
					}
	
					if os_list == "all" {
						os_refs, _ = refresh_creds( os_admin, os_refs, id2pname )						// periodic update of project cred list
					}

					osif_sheep.Baa( 1, "credentials were updated from openstack" )
				}

	/* ---- before lite ----
			case REQ_VM2IP:														// driven by tickler; gen a new vm translation map and push to net mgr
				m := mapvm2ip( os_refs )
				if m != nil {
					count := 0;
					msg := ipc.Mk_chmsg( )
					msg.Send_req( nw_ch, nil, REQ_VM2IP, m, nil )					// send new map to network as it is managed there
					osif_sheep.Baa( 1, "VM2IP mapping updated from openstack" )
					for k, v := range m {
						osif_sheep.Baa( 2, "VM mapped: %s ==> %s", k, *v )
						count++;
					}
					osif_sheep.Baa( 2, "mapped %d VM names/IDs from openstack", count )
				}
	*/

			case REQ_IP2MACMAP:													// generate an ip to mac map for the caller and write on the channel
				if msg.Response_ch != nil {										// no sense going off to ostack if no place to send the list
					msg.Response_data, msg.State = get_ip2mac( os_refs, inc_tenant )
				} else {
					osif_sheep.Baa( 0, "WRN: no response channel for host list request" )
				}

			case REQ_CHOSTLIST:
				if msg.Response_ch != nil {										// no sense going off to ostack if no place to send the list
					msg.Response_data, msg.State = get_hosts( os_refs )
				} else {
					osif_sheep.Baa( 0, "WRN: no response channel for host list request" )
				}

			case REQ_VALIDATE_HOST:						// validate and translate a [token/]project-name/host  string
				if msg.Response_ch != nil {
					msg.Response_data, msg.State = validate_token( msg.Req_data.( *string ), os_refs, pname2id, req_token )
				}

			case REQ_XLATE_HOST:						// accepts a [token/][project/]host name and translate project to an ID
				if msg.Response_ch != nil {
					msg.Response_data, msg.State = validate_token( msg.Req_data.( *string ), os_refs, pname2id, false )		// same process as validation but token not required
				}

			case REQ_VALIDATE_ADMIN:					// validate an admin token passed in
				if msg.Response_ch != nil {
					msg.State = validate_admin_token( os_admin, msg.Req_data.( *string ), def_usr )
					msg.Response_data = ""
				}

			case REQ_PNAME2ID:							// user, project, tenant (what ever) name to ID
				if msg.Response_ch != nil {
					msg.Response_data = pname2id[*(msg.Req_data.( *string ))] 
					if msg.Response_data.( *string ) == nil  {						// maybe it was an ID that came in
						if id2pname[*(msg.Req_data.( *string ))] != nil {			// if in id map, then return the stirng (the id) they passed (#202)
							msg.Response_data = msg.Req_data.( *string )
						} else {
							msg.Response_data = nil									// couldn't translate 
						}
					} 
				} 
				

			default:
				osif_sheep.Baa( 1, "unknown request: %d", msg.Msg_type )
				msg.Response_data = nil
				if msg.Response_ch != nil {
					msg.State = fmt.Errorf( "osif: unknown request (%d)", msg.Msg_type )
				}
		}

		osif_sheep.Baa( 3, "processing request complete: %d", msg.Msg_type )
		if msg.Response_ch != nil {			// if a reqponse channel was provided
			msg.Response_ch <- msg			// send our result back to the requestor
		}
	}
}
