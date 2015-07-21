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
				21 Aug 2014 - Fixed cause of core dump (ln 148) (steer)
				30 Sep 2014 - For what ever reason, the ccp environment wasn't returning a
					full complement of mac addresses on  a single call, so we now revert to
					making a call for each project.
				02 Oct 2014 - TGUOSI007 message eliminated as it duplcated 005.
				14 Oct 2014 - Corrected error count reset in gen_maps. Added additional check
					to ensure that empty maps are ignored.
				17 Nov 2014 - Changes to allow for lazy updating of maps.
					Side effects of this change:
						- project will always be included with VM name (not config file optional
						  with include_tenant setting which is now ignored.
						- request for ip2mac table by fqmgr is used only to accept a channel
						  and the map is pushed back when we think we have changes.
				04 Dec 2014 - Changed list host call to the list enabled host call in an attempt
						to use a list of active (up) hosts rather than every host known to 
						openstack.
				05 Dec 2014 - Added work round for AIC admin issue after they flipped to LDAP.
				16 Jan 2014 - Support port masks in flow-mods.
				18 Feb 2015 - Corrected slice index bug (@214)
				27 Feb 2015 - To make steering work with lazy updates.
				31 Mar 2015 - Changes to provide a force load of all VMs into the network graph.
				01 Apr 2015 - Corrected bug introduced by requiring a validate token to have a non
						empty host which is legit for steering.
				16 Apr 2015 - Pick up and use a region parameter from the config file.
				02 Jul 2015 - No longer bail from host list when a request turns up empty.
				13 Jul 2015 - Added work round openstack v2/v3 keystone issue with role verification
						now must try all roles in our known universe.
				21 Jul 2015 - Extended has_role function to accept either a token or token/project pair.

	Deprecated messages -- do NOT resuse the number as it already maps to something in ops doc!
				osif_sheep.Baa( 0, "WRN: no response channel for host list request  [TGUOSI011] DEPRECATED MESSAGE" )
*/

package managers

import (
	"fmt"
	"os"
	"strings"
	"time"

	"codecloud.web.att.com/gopkgs/bleater"
	"codecloud.web.att.com/gopkgs/clike"
	"codecloud.web.att.com/gopkgs/ipc"
	"codecloud.web.att.com/gopkgs/ostack"
	"codecloud.web.att.com/gopkgs/token"
	"codecloud.web.att.com/tegu/gizmos"
)

//var (
// NO GLOBALS HERE; use globals.go because of the shared namespace
//)


// --- Private --------------------------------------------------------------------------

/*
	Accept a token and a list of creds and try to determine the project that the token was
	generated for.  We'll first attempt to use the reference creds, but as with some 
	installations of openstack this seems not to work (AIC after LDAP was installed), so
	if using reference creds fails, we'll (cough) run the list of other creds, yes making
	an API call for each, until we find one that works or we exhaust the list.  Bottom line
	is that we'll fail only if we cannot figure it out someway.

	Dispite openstack doc, which implies that a token has a project scope, it does not.
	this forces us to loop through every project we know about to see if the token is 
	valid for the user and the project.   This could very easily return the wrong 
	project if the token can be used to access more than one set of project information. 

	Bottom line: use of this should be avoided!
*/
func token2project(  os_refs map[string]*ostack.Ostack, token *string ) ( pname *string, idp *string, err error ) {
	ostk := os_refs["_ref_"]					// first attempt: use our reference creds to examine the token
	if ostk != nil {
		pname, idp, err =  ostk.Token2project( token )
		if pname != nil {
			return
		}
	}

	for key, ostk := range os_refs {
		if key != "_ref_" {
			pname, idp, err =  ostk.Token2project( token )
			if pname != nil {
				osif_sheep.Baa( 2, "unable to validate token with our reference creds, finally successful with: %s", *(ostk.Get_user()) )
				return
			}
		}
	}

	osif_sheep.Baa( 2, "unable to validate token with any set of creds: %s", err )
	return
}
			

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

		case 3:													// could be: token/project/name, token/project/ID, token//ID,  !//IP-addr
			if tokens[0] == ""  {								// must have something out front, a ! or token, but empty is no good ([2] can be empty)
					return nil, fmt.Errorf( "invalid host name; expected {!|tok}/[project]/hostname, got: %s", *raw )
			}

			if tokens[1] == "" {								// empty project name, must attempt to extract from the token
				if tokens[0] != "!" {							//  if !//stuff we leave things alone and !//stuff is returned later
					pname, idp, err :=  token2project( os_refs, &tokens[0] )	// generate the project name and it's id from token
				
					if pname == nil {			// not a valid token, bail now
						return nil, err //fmt.Errorf( "invalid token" )
					}

					xstr := fmt.Sprintf( "%s/%s", *idp, tokens[2] )		// valid token, build id/host return string and send back
					osif_sheep.Baa( 2, "validation: %s: tok//host ==> %s", *raw, xstr )
					return &xstr, nil
				}
			} else {
				if pname2id != nil {
					idp = pname2id[tokens[1]]
				}
			}

			if idp == nil {					// assume it's already an id and needs no translation, or is empty and that's ok
				id = tokens[1]
			} else {
				id = *idp
			}

			if ! tok_req {										// if token required is false, then using this for translation, so skip the osif call
				xstr := fmt.Sprintf( "%s/%s", id, tokens[2] )	// build and return the translated string
				return &xstr, nil
			}

			if tokens[0][0:1] == "!"	{						// special indication to skip validation and return ID with a lead bang indicating not authorised
				xstr := fmt.Sprintf( "!%s/%s", id, tokens[2] )	// build and return the translated string
				osif_sheep.Baa( 2, "validation: unvalidated %s ==> %s", *raw, xstr )
				return &xstr, nil
			}

			pname, idp, err :=  token2project( os_refs, &tokens[0] )		// generate project name and id from the token
			if pname == nil {
				if err != nil {
					return nil, fmt.Errorf( "unable to determine project from token: %s", err )
				} else {
					return nil, fmt.Errorf( "unable to determine project from token: no diagnostic" )
				}
			}
			if *pname != tokens[1] && *idp != tokens[1] {			// must try both
				osif_sheep.Baa( 1, "invalid token/tenant: expected %s opnestack reports: %s/%s", tokens[1], *pname, *idp )
				return nil, fmt.Errorf( "invalid token/tenant pair" )
			}

			xstr := fmt.Sprintf( "%s/%s", id, tokens[2] )			// build and return the translated string
			osif_sheep.Baa( 2, "validation: %s: proj/host ==> %s", *raw, xstr )
			return &xstr, nil
	}
	
	return nil, fmt.Errorf( "invalid token/tenant pair" )
}

/*
	Verifies that the token passed in is a valid token for the default user (a.k.a. the tegu admin) given in the
	config file.
	Returns "ok" (err is nil) if it is good, and an error otherwise. 	
*/
func validate_admin_token( admin *ostack.Ostack, token *string, user *string ) ( error ) {

	osif_sheep.Baa( 2, "validating admin token" )
	exp, err := admin.Token_validation( token, user ) 		// ensure token is good and was issued for user
	if err == nil {
		osif_sheep.Baa( 2, "admin token validated successfully: %s expires: ", *token, exp )
	} else {
		osif_sheep.Baa( 1, "admin token invalid: %s", err )
	}

	return err
}


/*
	Given a token, return true if the token is valid for one of the roles listed in role.
	Role is a list of space separated role names.  If token is actually tokken/project, then
	we will test only with the indicated project. Otherwise we will test the token against
	every project we know about and return true if any of the roles in the list is defined
	for the user in any project.  This _is_ needed to authenticate Tegu requests which are not
	directly project oriented (e.g. set capacities, graph, etc.), so it is legitimate for
	a token to be submitted without a leading project/ string.
*/
func has_any_role( os_refs map[string]*ostack.Ostack, admin *ostack.Ostack, token *string, roles *string ) ( has_role bool, err error ) {
	rtoks := strings.Split( *roles, "," )		// simple tokenising of role list

	has_role = false
	if strings.Contains( *token, "/" ) {				// assume it's token/project
		const p int = 1			// order in split tokens (project)
		const t int = 0			// order in split tokens (actual token)

		toks := strings.Split( *token, "/" )
		if toks[p] == "" {
			osif_sheep.Baa( 2, "has_any_role: project/token had empty project" )
			return false, fmt.Errorf( "project portion of token/project was empty" )
		}	

		stuff, err := admin.Crack_ptoken( &toks[t], &toks[p], false )			// crack user info based on project and token
		if err == nil {
			state := gizmos.Map_has_any( stuff.Roles, rtoks )				// true if any from rtoks list matches any in Roles
			if state {
				osif_sheep.Baa( 2, "has_any_role: token/project validated for roles: %s", *roles )
				return true, nil
			} else {
				err = fmt.Errorf( "none matched" );
			}
		}

		osif_sheep.Baa( 2, "has_any_role: token/project not valid for roles: %s: %s", *roles, err )
		return false, fmt.Errorf( "has_any_role: token/project not valid for roles: %s: %s", roles, err )
	}

	for _, v := range os_refs {
		pname, _ := v.Get_project( )
		osif_sheep.Baa( 2, "has_any_role: checking %s", *pname )

		stuff, err := admin.Crack_ptoken( token, pname, false )			// return a stuff struct with details about the token

		if err == nil {
			err = fmt.Errorf( "role not defined; %s", *roles )			// asume error
			state := gizmos.Map_has_any( stuff.Roles, rtoks )			// true if any role token matches anything from ostack
			if state {
				osif_sheep.Baa( 2, "has_any_role: verified in %s", *pname )
				return true, nil
			}
		} else {
			osif_sheep.Baa( 2, "has_any_role: crack failed for project=%s: %s", *pname, err )
		}
	}

	if err == nil {
		err = fmt.Errorf( "undetermined reason" )
	}

	osif_sheep.Baa( 1, "unable to verify role: %s: %s", *roles, err )
	return false, err
}

func mapvm2ip( admin *ostack.Ostack, os_refs map[string]*ostack.Ostack ) ( m  map[string]*string ) {
	var (
		err	error
	)
	
	m = nil
	for k, ostk := range os_refs {
		if k != "_ref_" {	
			osif_sheep.Baa( 2, "mapping VMs from: %s", ostk.To_str( ) )
			m, err = ostk.Mk_vm2ip( m )
			if err != nil {
				osif_sheep.Baa( 1, "WRN: mapvm2ip: openstack query failed: %s   [TGUOSI000]", err )
				ostk.Expire()			// force re-auth next go round
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
		err = fmt.Errorf( "no openstack creds in list to query" )
		return
	}

	
	osif_sheep.Baa( 2, "physical host query starts: %d sets of creds", len( os_refs ) )
	for k, ostk := range os_refs {
		bs_class := fmt.Sprintf( "osif_gh_%s", k )			// baa_some class for this project

	osif_sheep.Baa( 2, "physical host query for %s", k )
		if k != "_ref_" {
			list, err = ostk.List_enabled_hosts( ostack.COMPUTE | ostack.NETWORK )	
			osif_sheep.Baa( 2, "physical host query for %s err is nil %v", k, err == nil )
			if err != nil {
				osif_sheep.Baa_some( bs_class, 100, 1, "WRN: error accessing host list: for %s: %s   [TGUOSI001]", ostk.To_str(), err )
				//ostk.Expire()					// force re-auth next go round
			} else {
				osif_sheep.Baa_some_reset( bs_class )			// reset on good attempt so 1st failure after good is logged
				if *list != "" {
					ts += sep + *list
					sep = " "
					osif_sheep.Baa( 2, "list of hosts was returned by %s  ", ostk.To_str() )	
				} else {
					osif_sheep.Baa( 2, "WRN: list of hosts not returned by %s   [TGUOSI002]", ostk.To_str() )	
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

	osif_sheep.Baa( 2, "phys host query ends: %d hosts", len( cmap ) )
	s = &ts
	return
}

/*
	Generate a map containing the translation from IP address to MAC address.
	Must run them all because in ccp we don't get everything with one call.

	Converted to get from project maps rather than an openstack call
*/
func get_ip2mac( os_projs map[string]*osif_project ) ( m map[string]*string, err error ) {
	
	err = nil
	m = make( map[string]*string )
	for _, p := range os_projs {
		p.Fill_ip2mac( m )				// add this project's translation map
	}

/*
	m = nil
	for k, ostk := range os_refs {
		if k != "_ref_" {
			m, _, err = ostk.Mk_mac_maps( m, nil, true )	
			if err != nil {
				osif_sheep.Baa( 1, "WRN: unable to map MAC info: %s; %s   [TGUOSI005]", os_refs["_ref_"].To_str( ), err )
				ostk.Expire()					// force re-auth next go round
				return
			}
		}
	}
*/

	return
}

/*
	Gets an openstack interface object for the admin user (tegu user id as defined in the config file).
	This function blocks until it gets them AND can successfully authenticate.
*/
func get_admin_creds( url *string, usr *string, passwd *string, project *string, region *string ) ( creds *ostack.Ostack ) {
	creds = nil

	if url == nil || usr == nil || passwd == nil {
		osif_sheep.Baa( 1, "cannot generate default tegu creds: no url, usr and/or password" );
		return
	}

	creds = ostack.Mk_ostack_region( url, usr, passwd, project, region )		// project isn't known or needed for this

	if creds == nil {
		osif_sheep.Baa( 1, "cannot generate default tegu creds: nil returned from library call" )
		return 
	}

	for {
		err := creds.Authorise()		// must ensure we can authorise before we can continue
		if err == nil {
			r_str := "default"
			if region != nil {
				r_str = *region
			}
			osif_sheep.Baa( 1, "tegu user (admin) creds were allocated and authorised with region: %s", r_str )
			return
		}

		osif_sheep.Baa( 1, "unable to authenticate tegu (admin) creds: %s", err )
		time.Sleep( time.Second * 60 )
	}
}

/*
	Build a set of openstack objects for each project (tenant) that we have access to.
	Retuns the list of creds and both ID->project and project->ID maps

	We build a new map each time, copying existing references, so that if a parallel thread
	has a copy and is working from it a change to the map isn't disruptive.

	This function also sets a reference ("_ref_") entry in the map which can be used to pull
	an entry out when any of them will do.

	NOTE: All errors will be logged, but only the first error will be returned to the caller.
*/
func refresh_creds( admin *ostack.Ostack, old_list map[string]*ostack.Ostack, id2pname map[string]*string ) ( creds map[string]*ostack.Ostack, gerr error ) {
	var (
		r	*ostack.Ostack
		err	error
	)

	creds = make( map[string]*ostack.Ostack )			// new map to fill in
	if old_list == nil {
		old_list = creds
	}

	r = nil
	for k, v := range id2pname {						// run the list of projects and add creds to the map if we don't have them
		if old_list[*v] == nil  {	
			osif_sheep.Baa( 1, "adding creds for: %s/%s", k, *v )
			creds[*v], err = admin.Dup( v )				// duplicate creds for this project and then authorise to get a token
	
			if err != nil {
				osif_sheep.Baa( 1, "WRN: unable to authorise credentials for project: %s   [TGUOSI008]", *v )
				delete( creds, *v )
			}
			if gerr == nil {			// ensure error captured for return if last in list is good
				gerr = err
			}
		} else {
			creds[*v] = old_list[*v]					// reuse the data
			osif_sheep.Baa( 2, "reusing credentials for: %s", *v )
		}

		if r == nil &&  creds[*v] != nil {							// need to test if this has admin -- ref must have admin
 			_, _, err = creds[*v].Mk_mac_maps( nil, nil, false )
			if  err == nil  {
				r = creds[*v]
			}
		}
	}

	creds["_ref_"] = r				// set the reference entry

	return
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
		os_refs		map[string]*ostack.Ostack	// creds for each project we need to request info from
		os_projects map[string]*osif_project	// list of project info (maps)
		os_admin	*ostack.Ostack				// admin creds
		refresh_delay	int = 15				// config file can override
		id2pname	map[string]*string			// project id/name translation maps
		pname2id	map[string]*string
		req_token	bool = false				// if set to true in config file the token _must_ be present when called to validate
		def_passwd	*string						// defaults and what we assume are the admin creds
		def_usr		*string
		def_url		*string
		def_project	*string
		def_region	*string
	)

	osif_sheep = bleater.Mk_bleater( 0, os.Stderr )		// allocate our bleater and attach it to the master
	osif_sheep.Set_prefix( "osif_mgr" )
	tegu_sheep.Add_child( osif_sheep )					// we become a child so that if the master vol is adjusted we'll react too

	//ostack.Set_debugging( 0 );

	// ---- pick up configuration file things of interest --------------------------

	if cfg_data["osif"] != nil {								// cannot imagine that this section is missing, but don't fail if it is
		def_passwd = cfg_data["osif"]["passwd"]				// defaults applied if non-section given in list, or info omitted from the section
		def_usr = cfg_data["osif"]["usr"]
		def_url = cfg_data["osif"]["url"]
		def_project = cfg_data["osif"]["project"]
	
		p := cfg_data["osif"]["refresh"]
		if p != nil {
			refresh_delay = clike.Atoi( *p ); 			
			if refresh_delay < 15 {
				osif_sheep.Baa( 1, "resresh was too small (%ds), setting to 15", refresh_delay )
				refresh_delay = 15
			}
		}
	
		p = cfg_data["osif"]["debug"]
		if p != nil {
			v := clike.Atoi( *p )
			if v > -5 {
				ostack.Set_debugging( v )
			}
		}

		p = cfg_data["osif"]["region"] 
		if p != nil {
			def_region = p
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
		}

		p = cfg_data["osif"]["verbose"]
		if p != nil {
			osif_sheep.Set_level(  uint( clike.Atoi( *p ) ) )
		}
	}

	if os_list == " " || os_list == "" || os_list == "off" {
		osif_sheep.Baa( 0, "osif disabled: no openstack list (ostack_list) defined in configuration file or setting is 'off'" )
	} else {
		// TODO -- investigate getting id2pname maps from each specific set of creds defined if an overarching admin name is not given

		os_admin = get_admin_creds( def_url, def_usr, def_passwd, def_project, def_region )		// this will block until we authenticate
		if os_admin != nil {
			osif_sheep.Baa( 1, "admin creds generated, mapping tenants" )
			pname2id, id2pname, _ = os_admin.Map_tenants( )		// get the initial translation maps
			//pname2id, id2pname, _ = os_admin.Map_all_tenants( )		// get the translation maps
			for k, v := range pname2id {
				osif_sheep.Baa( 1, "project known: %s %s", k, *v )				// useful to see in log what projects we can see
			}
		} else {
			id2pname = make( map[string]*string )				// empty maps and we'll never generate a translation from project name to tenant ID since there are no default admin creds
			pname2id = make( map[string]*string )
			if def_project != nil {
				osif_sheep.Baa( 0, "WRN: unable to use admin information (%s, proj=%s, reg=%s) to authorise with openstack  [TGUOSI009]", def_usr, def_project, def_region )
			} else {
				osif_sheep.Baa( 0, "WRN: unable to use admin information (%s, proj=no-project, reg=%s) to authorise with openstack  [TGUOSI009]", def_usr, def_region )	// YES msg ids are duplicated here
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

		os_projects = make( map[string]*osif_project )
		add2projects( os_projects, os_refs, pname2id, 0 )							// add refernces to the projects list
	}

	// ---------------- end config parsing ----------------------------------------


	//tklr.Add_spot( 3, my_chan, REQ_GENMAPS, nil, 1 )						// add tickle spot to drive us once in 3s and then another to drive us based on config refresh rate
	//tklr.Add_spot( int64( refresh_delay ), my_chan, REQ_GENMAPS, nil, ipc.FOREVER );  	
	tklr.Add_spot( 3, my_chan, REQ_GENCREDS, nil, 1 )						// add tickle spot to drive us once in 3s and then another to drive us based on config refresh rate
	tklr.Add_spot( int64( 180 ), my_chan, REQ_GENCREDS, nil, ipc.FOREVER );  	

	osif_sheep.Baa( 2, "osif manager is running  %x", my_chan )
	for {
		msg = <- my_chan					// wait for next message from tickler
		msg.State = nil						// default to all OK

		osif_sheep.Baa( 3, "processing request: %d", msg.Msg_type )
		switch msg.Msg_type {
			case REQ_GENMAPS:								// driven by tickler
					// deprecated with switch to lazy update

			case REQ_GENCREDS:								// driven by tickler
				if os_admin != nil {
					//new_name2id, new_id2pname, err := os_admin.Map_tenants( )		// fetch new maps, overwrite only if no errors
					new_name2id, new_id2pname, err := os_admin.Map_all_tenants( )		// fetch new maps, overwrite only if no errors
					if err == nil {
						pname2id = new_name2id
						id2pname = new_id2pname
					} else {
						osif_sheep.Baa( 1, "WRN: unable to get tenant name/ID translation data: %s  [TGUOSI010]", err )
					}
	
					if os_list == "all" {
						os_refs, _ = refresh_creds( os_admin, os_refs, id2pname )						// periodic update of project cred list
						add2projects( os_projects, os_refs, pname2id, 2 )								// add refernces to the projects list
					}

					osif_sheep.Baa( 2, "credentials were updated from openstack" )
				}

	/* ---- before lite ----
			case REQ_VM2IP:														// driven by tickler; gen a new vm translation map and push to net mgr
				m := mapvm2ip( os_refs )
				if m != nil {
					count := 0;
					msg := ipc.Mk_chmsg( )
					msg.Send_req( nw_ch, nil, REQ_VM2IP, m, nil )					// send new map to network as it is managed there
					osif_sheep.Baa( 2, "VM2IP mapping updated from openstack" )
					for k, v := range m {
						osif_sheep.Baa( 3, "VM mapped: %s ==> %s", k, *v )
						count++;
					}
					osif_sheep.Baa( 2, "mapped %d VM names/IDs from openstack (verbose 3 for debug list)", count )
				}
	*/

			case REQ_IP2MACMAP:												// generate an ip to mac map and send to those who need it (fq_mgr at this point)
				freq := ipc.Mk_chmsg( )										// need a new request to pass to fq_mgr
				data, err := get_ip2mac( os_projects )
				if err == nil {
					osif_sheep.Baa( 2, "sending ip2mac map to fq_mgr" )
					freq.Send_req( fq_ch, nil, REQ_IP2MACMAP, data, nil )	// request data forward
					msg.State = nil											// response ok back to requestor
				} else {
					msg.State = err											// error goes back to requesting process
				}

			case REQ_CHOSTLIST:
				if msg.Response_ch != nil {										// no sense going off to ostack if no place to send the list
					osif_sheep.Baa( 2, "starting list host" )
					msg.Response_data, msg.State = get_hosts( os_refs )
					osif_sheep.Baa( 2, "finishing list host" )
				} else {
					osif_sheep.Baa( 0, "WRN: no response channel for host list request  [TGUOSI012]" )
				}

/* ======= don't think these are needed but holding ======
			case REQ_PROJNAME2ID:					// translate a project name (tenant) to ID
				if msg.Response_ch != nil {
					pname := msg.Req_data.( *string )
					if s, ok := pname2id[*pname]; ok {			// translate if there, else assume it's in it's "final" form
						msg.Response_data = s
					} else {
						msg.Response_data = pname
					}
				}
				
*/

			case REQ_VALIDATE_TOKEN:						// given token/tenant validate it and translate tenant name to ID if given; returns just ID
				if msg.Response_ch != nil {
					s := msg.Req_data.( *string )
					*s += "/"								// add trailing slant to simulate "data"
					msg.Response_data, msg.State = validate_token( s, os_refs, pname2id, req_token )
				}


			case REQ_GET_HOSTINFO:						// dig out all of the bits of host info for a single host from oepnstack and return in a network update struct
				if msg.Response_ch != nil {
					go get_os_hostinfo( msg, os_refs, os_projects, id2pname, pname2id )			// do it asynch and return the result on the message channel
					msg = nil							// prevent early response
				}

			case REQ_GET_PROJ_HOSTS:
				if msg.Response_ch != nil {
					go get_all_osvm_info( msg, os_refs, os_projects, id2pname, pname2id )		// do it asynch and return the result on the message channel
					msg = nil																	// prevent response from this function
				}

			case REQ_GET_DEFGW:							// dig out the default gateway for a project
				if msg.Response_ch != nil {
					go get_os_defgw( msg, os_refs, os_projects, id2pname, pname2id )			// do it asynch and return the result on the message channel
					msg = nil							// prevent early response
				}

			case REQ_VALIDATE_HOST:						// validate and translate a [token/]project-name/host  string
				if msg.Response_ch != nil {
					msg.Response_data, msg.State = validate_token( msg.Req_data.( *string ), os_refs, pname2id, req_token )
				}

			case REQ_XLATE_HOST:						// accepts a [token/][project/]host name and translate project to an ID
				if msg.Response_ch != nil {
					msg.Response_data, msg.State = validate_token( msg.Req_data.( *string ), os_refs, pname2id, false )		// same process as validation but token not required
				}

			case REQ_VALIDATE_TEGU_ADMIN:					// validate that the token is for the tegu user
				if msg.Response_ch != nil {
					msg.State = validate_admin_token( os_admin, msg.Req_data.( *string ), def_usr )
					msg.Response_data = ""
				}

			case REQ_HAS_ANY_ROLE:							// given a token and list of roles, returns true if any role listed is listed by openstack for the token
				if msg.Response_ch != nil {
					d := msg.Req_data.( *string )
					dtoks := strings.Split( *d, " " )					// data assumed to be token <space> role[,role...]
					if len( dtoks ) > 1 {
						msg.Response_data, msg.State = has_any_role( os_refs, os_admin, &dtoks[0], &dtoks[1] )
					} else { 
						msg.State = fmt.Errorf( "has_any_role: bad input data" )
						msg.Response_data = false
					}

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

		if msg != nil  { 						// if msg wasn't passed off to a go routine
			osif_sheep.Baa( 3, "processing request complete: %d", msg.Msg_type )

			if msg.Response_ch != nil {			// if a reqponse channel was provided
				msg.Response_ch <- msg			// send our result back to the requestor
			}
		}
	}
}
