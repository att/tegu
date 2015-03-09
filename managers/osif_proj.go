
// vi: sw=4 ts=4:

/*
	Mnemonic:	osif_proj.go
	Abstract:	Functions that manage an osif project struct. For now it manages the
				related translation maps for the project. In future it might also
				be used to reference the associated creds, but not wanting to change
				the structure that builds that aspect of thigs.

	Date:		17 November 2014
	Author:		E. Scott Daniels

	Mods:		16 Dec 2014 - Corrected slice out of bounds error in get_host_info()
				09 Jan 2015 - No longer assume that the gateway list is limited by the project
					that is valid in the creds.  At least some versions of Openstack were
					throwing all gateways into the subnet list.
				16 Jan 2014 - Support port masks in flow-mods.
				26 Feb 2014 - Added support to dig out the default gateway for a project.
*/

package managers
// should this move to gizmos?

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"codecloud.web.att.com/gopkgs/clike"
	"codecloud.web.att.com/gopkgs/ipc"
	"codecloud.web.att.com/gopkgs/ostack"
)


type osif_project struct {
	name		*string
	lastfetch	int64						// timestamp of last map update to detect freshness
	vmid2ip		map[string]*string			// translation maps for the project
	ip2vmid		map[string]*string
	ip2vm		map[string]*string
	vm2ip		map[string]*string			// vm name to ip; gateway IPs are used as names
	vmid2host	map[string]*string
	ip2mac		map[string]*string
	gwmap		map[string]*string			// mac to ip translation
	ip2fip		map[string]*string
	fip2ip		map[string]*string
	gw2cidr		map[string]*string

	rwlock		sync.RWMutex						// must lock to prevent update collisions
}

// -------------------------------------------------------------------------------------------------

/*	Given an IP address and a network address and number of bits test to
	see if the IP address is in the subnet. Returns true if the IP address
	is in the subnet. */
func in_subnet( ip string, net string, nbits int ) ( bool ) {
	var (
		mask int64
		ipv int64
		netv int64
	)

	if nbits > 0 {									// if net bits given, compute the mask and compare
		ipt := strings.Split( ip, "." )				// tokenise
		nett := strings.Split( net, "." )

		if len( ipt ) < 4 || len( nett ) < 4 {
			osif_sheep.Baa( 2, "in_subnet: bad/unsupported ip address or network address: %s %s", ip, net )
			return false
		}

		ipv = 0
		netv = 0
		for i := 0; i < 4; i++ {
			ipv <<= 8
			netv <<= 8
			
			ipv += clike.Atoll( ipt[i] )
			netv += clike.Atoll( nett[i] )
		}

		mask = 0
		for i := 0; i < nbits; i++ {
			mask = (mask >> 1) + 0x80000000
		}

		return (ipv & mask) ==  netv
	}

	return false
}

// -------------------------------------------------------------------------------------------------

/*
	Make a new project map management block.
*/
func Mk_osif_project( name string ) ( p *osif_project, err error ) {
	p = &osif_project {
		name:	&name,
		lastfetch:	0,
	}

	p.vmid2ip = make( map[string]*string )
	p.ip2vmid = make( map[string]*string )
	p.vm2ip = make( map[string]*string )
	p.ip2vm = make( map[string]*string )
	p.vmid2host = make( map[string]*string )
	p.ip2mac = make( map[string]*string )
	p.gwmap = make( map[string]*string )
	p.ip2fip = make( map[string]*string )
	p.fip2ip = make( map[string]*string )

	return
}

/*
	Build all translation maps for the given project.
	Does NOT replace a map with a nil map; we assume this is an openstack glitch.

	CAUTION:  ip2 maps are complete, where vm2 or vmid2 maps are not because
			they only reference one of the VMs IP addresses where there might
			be many.
*/
func (p *osif_project) refresh_maps( creds *ostack.Ostack ) ( rerr error ) {
	
	if p == nil {
		return
	}
	if creds == nil {
		osif_sheep.Baa( 1, "IER: refresh_maps given nil creds" )
		rerr = fmt.Errorf( "creds were nil" )
		return
	}

	if *p.name != "_ref_" {				// we don't fetch maps from the ref since it's not real
		olastfetch := p.lastfetch		// last fetch -- ensure it wasn't fetched while we waited
		p.rwlock.Lock()					// wait for a write lock
		defer p.rwlock.Unlock()				// ensure unlocked on return

		if olastfetch != p.lastfetch {	// assume read done while we waited
			return
		}

		osif_sheep.Baa( 2, "refresh: creating VM maps from: %s", creds.To_str( ) )
		vmid2ip, ip2vmid, vm2ip, vmid2host, vmip2vm, err := creds.Mk_vm_maps( nil, nil, nil, nil, nil, true )
		if err != nil {
			osif_sheep.Baa( 2, "WRN: unable to map VM info (vm): %s; %s   [TGUOSI003]", creds.To_str( ), err )
			rerr = err
			creds.Expire()					// force re-auth next go round
		} else {
			osif_sheep.Baa( 2, "%s map sizes: vmid2ip=%d ip2vmid=%d vm2ip=%d vmid2host=%d vmip2vm=%d", 
					*p.name, len( vmid2ip ), len( ip2vmid ), len( vm2ip ), len( vmid2host ), len( vmip2vm ) )
			if len( vmip2vm ) > 0 && len( vmid2ip ) > 0 &&  len( ip2vmid ) > 0 &&  len( vm2ip ) > 0 &&  len( vmid2host ) > 0  {		// don't refresh unless all are good
				p.vmid2ip = vmid2ip						// id and vm name map to just ONE ip address
				p.vm2ip = vm2ip
				p.ip2vmid = ip2vmid						// the only complete list of ips
				p.vmid2host = vmid2host					// id to physical host
				p.ip2vm = vmip2vm
			}
		}

		fip2ip, ip2fip, err := creds.Mk_fip_maps( nil, nil, true )
		if err != nil {
			osif_sheep.Baa( 2, "WRN: unable to map VM info (fip): %s; %s   [TGUOSI004]", creds.To_str( ), err )
			rerr = err
			creds.Expire()					// force re-auth next go round
		} else {
			osif_sheep.Baa( 2, "%s map sizes: ip2fip=%d fip2ip=%d", *p.name, len( ip2fip ), len( fip2ip ) )
			if len( ip2fip ) > 0 &&  len( fip2ip ) > 0 {
				p.ip2fip = ip2fip
				p.ip2fip = fip2ip
			}
		}

		ip2mac, _, err := creds.Mk_mac_maps( nil, nil, true )	
		if err != nil {
			osif_sheep.Baa( 2, "WRN: unable to map MAC info: %s; %s   [TGUOSI005]", creds.To_str( ), err )
			rerr = err
			creds.Expire()					// force re-auth next go round
		} else {
			osif_sheep.Baa( 2, "%s map sizes: ip2mac=%d", *p.name, len( ip2mac ) )
			if len( ip2mac ) > 0  {
				p.ip2mac = ip2mac
			}
		}
	

		gwmap, _, gwmac2id, _, _, gwip2phost, err := creds.Mk_gwmaps( nil, nil, nil, nil, nil, nil, true, false )		// gwmap is mac2ip
		if err != nil {
			osif_sheep.Baa( 2, "WRN: unable to map gateway info: %s; %s   [TGUOSI006]", creds.To_str( ), err )
			creds.Expire()					// force re-auth next go round
		} else {
			osif_sheep.Baa( 2, "%s map sizes: gwmap=%d", *p.name, len( gwmap ) )
			if len( gwmap ) > 0 {
				p.gwmap = gwmap
			}

			for mac, id := range gwmac2id {			// run the gateway info and insert as though they were first class VMs
				ip := gwmap[mac]
				p.vmid2ip[*id] = ip
				p.ip2vmid[*ip] = id
				p.vmid2host[*id] = gwip2phost[*ip]
				p.vm2ip[*ip] = ip				// gw is nameless, so use the ip address
			}
		}

		_, gw2cidr, err := creds.Mk_snlists( ) 		// get list of gateways and their subnet cidr
		if err == nil && gw2cidr != nil {
			p.gw2cidr = gw2cidr
		} else {
			if err != nil {
				osif_sheep.Baa( 1, "WRN: unable to create gateway to cidr map: %s; %s   [TGUOSI007]", creds.To_str( ), err )
			} else {
				osif_sheep.Baa( 1, "WRN: unable to create gateway to cidr map: %s  no reason given   [TGUOSI007]", creds.To_str( ) )
			}
			creds.Expire()					// force re-auth next go round
		}

		p.lastfetch = time.Now().Unix()
	}

	return
}

/* Suss out the gateway from the list based on the VM's ip address.
	Must look at the project on the gateway as some flavours of openstack
	seem to return every subnet, not just the subnets defined for the
	project listed in the creds.
*/
func (p *osif_project) ip2gw( ip4 *string ) ( *string ) {
	if p == nil || ip4 == nil {
		return nil
	}

	ip_toks := strings.Split( *ip4, "/" )			// assume project/ip
	ip := ""
	project := ""
	if len( ip_toks ) > 1 {						// should always be 2, but don't core dump if not
		ip = ip_toks[1]
		project = ip_toks[0]					// capture the project for match against the gateway
	} else {
		ip = ip_toks[0]
	}
		
	for k, v := range p.gw2cidr {												// key is the project/ip of the gate, value is the cidr
		k_toks := strings.Split( k, "/" )										// need to match on project too
		if len( k_toks ) == 1  ||  k_toks[0] ==  project || project == "" {		// safe to check the cidr
			c_toks := strings.Split( *v, "/" )
			if in_subnet( ip, c_toks[0], clike.Atoi( c_toks[1] ) ) {
				osif_sheep.Baa( 1, "mapped ip to gateway for: %s  %s", *ip4, k )
				return &k
			}
		}
	}

	osif_sheep.Baa( 1, "osif-ip2gw: unable to map ip to gateway for: %s", *ip4 )
	return nil
}

/* Suss out the first gateway (router) for the project. Needed for E* steering case.
	Assume input (proj_stuff) is either project, project/, or project/<stuff>.
*/
func (p *osif_project) suss_default_gw( proj_stuff *string ) ( *string ) {
	if p == nil || proj_stuff == nil {
		return nil
	}

	proj_toks := strings.Split( *proj_stuff, "/" )			// could be project/<stuff>; ditch stuff
	project := proj_toks[0]
		
	for k, _ := range p.gw2cidr {												// key is the project/ip of the gate, value is the cidr
		k_toks := strings.Split( k, "/" )										// need to match on project too
		if len( k_toks ) == 1  ||  k_toks[0] ==  project || project == "" {		// found the first, return it
			osif_sheep.Baa( 1, "found default gateway for: %s  %s", project, k )
			return &k
		}
	}

	osif_sheep.Baa( 1, "osif-ip2gw: unable to find default gateway for: %s", project )
	return nil
}

/*
	Supports Get_info by searching for the information but does not do a reload.
*/
func (p *osif_project) suss_info( search *string ) ( name *string, id *string, ip4 *string, fip4 *string, mac *string, gw *string, phost *string, gwmap map[string]*string ) {

	name = nil
	id = nil
	ip4 = nil

	if p == nil || search == nil {
		return
	}

	p.rwlock.RLock()							// lock for reading
	defer p.rwlock.RUnlock() 					// ensure unlocked on return

	if p.vm2ip[*search] != nil {				// search is the name
		ip4 = p.vm2ip[*search]
		name = search
	} else {
		if p.ip2vmid[*search] != nil {			// name is actually an ip
			ip4 = search
			id = p.ip2vmid[*ip4]
			name = p.ip2vm[*ip4]
		} else {								// assume its an id or project/id
			if p.vmid2ip[*search] != nil {		// id2ip shouldn't include project, but handle that case
				id = search
				ip4 = p.vmid2ip[*id]
				name = p.ip2vm[*ip4]
			} else {
				tokens := strings.Split( *search, "/" )			// could be id or project/id
				id = &tokens[0]									// assume it's just the id and not project/id
				if len( tokens ) > 1  {
					id = &tokens[1]
				}
				if p.vmid2ip[*id] != nil {
					ip4 = p.vmid2ip[*id]
					name = p.ip2vm[*ip4]
				}
			}
		}
	}

	if name == nil || ip4 == nil {
		return
	}

	if id == nil {
		id = p.ip2vmid[*ip4]
	}

	fip4 = p.ip2fip[*ip4]
	gw = p.ip2gw( ip4 )					// find the gateway for the VM
	mac = p.ip2mac[*ip4]
	phost = p.vmid2host[*id]
	gwmap = make( map[string]*string, len( p.gwmap ) )
	for k, v := range p.gwmap {
		gwmap[k] = v					// should be safe to reference the same string
	}

	return
}


/*
	Looks for the search string treating it first as a VM name, then VM IP address
	and finally VM ID (might want to redo that order some day) and if a match in
	the maps is found, we return the gambit of information.  If not found, we force
	a reload of the map and then search again.  The new-data flag indicates that the
	information wasn't in the previous map.
*/
func (p *osif_project) Get_info( search *string, creds *ostack.Ostack, inc_project bool ) (
		name *string,
		id *string,
		ip4 *string,
		fip4 *string,
		mac *string,
		gw *string,
		phost *string,
		gwmap map[string]*string,
		new_data bool,
		err error ) {

	new_data = false
	err = nil
	name = nil
	id = nil

	if creds == nil {
		err = fmt.Errorf( "creds were nil" )
		osif_sheep.Baa( 2, "lazy update: unable to get nil creds" )
		return
	}

	if time.Now().Unix() - p.lastfetch < 90 {					// if fresh, try to avoid reload
		name, id, ip4, fip4, mac, gw, phost, gwmap = p.suss_info( search )
	}

	if name == nil {											// not found or not fresh, force reload
		osif_sheep.Baa( 2, "lazy update: data reload for: %s", *p.name )
		new_data = true		
		err = p.refresh_maps( creds )
		if err == nil {
			name, id, ip4, fip4, mac, gw, phost, gwmap = p.suss_info( search )
		}
	}

	return
}

/* Public interface to get the default gateway (router) for a project. Causes data to 
	be loaded if stale.  Search is the project name or ID and can be of the form 
	project/<stuff> where stuff will be ignored. New data (return) is true if the data
	had to be loaded.
*/
func (p *osif_project) Get_default_gw( search *string, creds *ostack.Ostack, inc_project bool ) ( gw *string, new_data bool, err error ) {

	new_data = false
	err = nil
	gw = nil

	if creds == nil {
		err = fmt.Errorf( "creds were nil" )
		osif_sheep.Baa( 1, "lazy gw update: unable to get, nil creds" )
		return
	}

	if time.Now().Unix() - p.lastfetch < 90 {					// if fresh, try to avoid reload
		gw = p.suss_default_gw( search )
	}

	if gw == nil {											// not found or not fresh, force reload
		osif_sheep.Baa( 2, "lazy gw update: data reload for: %s", *p.name )
		new_data = true		
		err = p.refresh_maps( creds )
		if err == nil {
			gw = p.suss_default_gw( search )
		}
	}

	return
}

/*
	Fill in the ip2mac map that is passed in with ours. Must grab the read lock to make this
	safe.
*/
func (p *osif_project) Fill_ip2mac( umap map[string]*string ) {
	if umap == nil {
		return
	}

	p.rwlock.RLock()							// lock for reading
	defer p.rwlock.RUnlock() 					// ensure unlocked on return

	for k, v := range p.ip2mac {
		umap[k] = v
	}
}


// - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - -

/*
	Get openstack host information.
	Given a project-id/host as input, dig out all of the host's information and build a struct
	that can be passed into the network manager as an add host to graph request. This
	expects to run as a go routine and to write the response directly back on the channel
	givn in the message block.
*/
func get_os_hostinfo( msg	*ipc.Chmsg, os_refs map[string]*ostack.Ostack, os_projs map[string]*osif_project, id2pname map[string]*string, pname2id map[string]*string ) {
	if msg == nil || msg.Response_ch == nil {
		return															// prevent accidents
	}

	msg.Response_data = nil

	tokens := strings.Split( *(msg.Req_data.( *string )), "/" )			// break project/host into bits
	if len( tokens ) != 2 || tokens[0] == "" || tokens[1] == "" {
		osif_sheep.Baa( 1, "get hostinfo: unable to map to a project: %s bad tokens",  *(msg.Req_data.( *string )) )
		msg.State = fmt.Errorf( "invalid project/hostname string: %s", *(msg.Req_data.( *string )) )
		msg.Response_ch <- msg
		return
	}

	if tokens[0] == "!" { 					// !//ipaddress was given; we've got nothing, so bail now
		osif_sheep.Baa( 1, "get hostinfo: unable to map to a project: %s lone bang",  *(msg.Req_data.( *string )) )
		msg.Response_ch <- msg
		return
	}

	if tokens[0][0:1] == "!" {				// first character is a bang, but there is a name/id that follows
		tokens[0] = tokens[0][1:]			// ditch it for this
	}

	pid := &tokens[0]
	pname := id2pname[*pid]
	if pname == nil {						// it should be an id, but allow for a name/host to be sent in
		pname = &tokens[0]
		pid = pname2id[*pname]
	}

	if pid == nil {
		osif_sheep.Baa( 1, "get hostinfo: unable to map to a project: %s",  *(msg.Req_data.( *string )) )  // might be !project/vm, and so this is ok
		msg.State = fmt.Errorf( "%s could not be mapped to a osif_project", *(msg.Req_data.( *string )) )
		msg.Response_ch <- msg
		return
	}

	p := os_projs[*pid]
	if p == nil {
		msg.State = fmt.Errorf( "%s could not be mapped to a osif_project", *(msg.Req_data.( *string )) )
		msg.Response_ch <- msg
		return
	}

	creds := os_refs[*pname]
	if creds == nil {
		msg.State = fmt.Errorf( "%s could not be mapped to openstack creds ", *pname )
		msg.Response_ch <- msg
		return
	}
	
	osif_sheep.Baa( 2, "lazy update: get host info setup complete for (%s) %s", *pname, *(msg.Req_data.( *string )) )

	search := *pid + "/" + tokens[1]							// search string must be id/hostname
	name, id, ip4, fip4, mac, gw, phost, gwmap, _, err := p.Get_info( &search, creds, true )
	if err != nil {
		msg.State = fmt.Errorf( "unable to retrieve host info: %s", err )
		msg.Response_ch <- msg
		return
	}
	
	msg.Response_data = Mk_netreq_vm( name, id, ip4, nil, phost, mac, gw, fip4, gwmap )		// build the vm data block for network manager
	msg.Response_ch <- msg																// and send it on its merry way

	return
}


/* Get the default gateway for a project. Returns the string directly to the channel 
	that send the osif the message. Expects to be executed as  a go routine. 
go get_os_defgw( msg, os_refs, os_projects, id2pname, pname2id )			// do it asynch and return the result on the message channel
*/
func get_os_defgw( msg	*ipc.Chmsg, os_refs map[string]*ostack.Ostack, os_projs map[string]*osif_project, id2pname map[string]*string, pname2id map[string]*string ) {
	if msg == nil || msg.Response_ch == nil {
		return															// prevent accidents
	}

	msg.Response_data = nil

	if msg.Req_data != nil {
		tokens := strings.Split( *(msg.Req_data.( *string )), "/" )			// split off /junk if it's tehre
		if tokens[0] == "!" || tokens[0] == "" { 							// nothing to work with; bail now
			osif_sheep.Baa( 1, "get_defgw: unable to map to a project -- bad token[0] --: %s",  *(msg.Req_data.( *string )) )
			msg.Response_ch <- msg
			return
		}

		if tokens[0][0:1] == "!" {				// first character is a bang, but there is a name/id that follows
			tokens[0] = tokens[0][1:]			// ditch the bang and go on
		}

		pid := &tokens[0]
		pname := id2pname[*pid]
		if pname == nil {						// it should be an id, but allow for a name/host to be sent in
			osif_sheep.Baa( 1, "get_defgw: unable to map to a project -- no pname --: %s",  *(msg.Req_data.( *string )) )
			pname = &tokens[0]
			pid = pname2id[*pname]
		}

		if pid == nil {
			osif_sheep.Baa( 1, "get_defgw: unable to map to a project: %s",  *(msg.Req_data.( *string )) )
			msg.State = fmt.Errorf( "%s could not be mapped to a osif_project", *(msg.Req_data.( *string )) )
			msg.Response_ch <- msg
			return
		}

		p := os_projs[*pid]						// finally we can find the data associated with the project; maybe
		if p == nil {
			osif_sheep.Baa( 1, "get_defgw: unable to map project to data: %s", *pid )
			msg.State = fmt.Errorf( "%s could not be mapped to a osif_project", *(msg.Req_data.( *string )) )
			msg.Response_ch <- msg
			return
		}

		creds := os_refs[*pname]
		if creds == nil {
			msg.State = fmt.Errorf( "defgw: %s could not be mapped to openstack creds ", *pname )
			msg.Response_ch <- msg
			return
		}

		msg.Response_data, _, msg.State = p.Get_default_gw( pid, creds, true )
		msg.Response_ch <- msg
		return
	}
	
	osif_sheep.Baa( 1, "get_defgw:  missing data (nil) in request" )
	msg.State = fmt.Errorf( "defgw: missing data in request" )
	msg.Response_ch <- msg																// and send it on its merry way

	return
}
