// vi: sw=4 ts=4:
/*
 ---------------------------------------------------------------------------
   Copyright (c) 2013-2015 AT&T Intellectual Property

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at:

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
 ---------------------------------------------------------------------------
*/


/*

	Mnemonic:	pledge_mirror
	Abstract:	A pledge for a mirror.
				Now that a pledge has been converted to an interface, this needs to be

	Date:		17 Feb 2015
	Author:		Robert Eby

	Mods:		17 Feb 2015 - Added mirroring
				26 May 2015 - Broken out of main pledge to allow for pledge to become an interface.
				01 Jun 2015 - Added equal() support
				16 Aug 2015 - Move common code into Pledge_base
				16 Nov 2015 - Add tenant_id, stdout, stderr to Pledge_mirror
				24 Nov 2015 - Add options
				25 Feb 2016 - Correct formatting issue in json output.
*/

package gizmos

import (
	"encoding/json"
	"fmt"
	"strings"
)

// needs rework to rename fields that make sense to mirroring
type Pledge_mirror struct {
				Pledge_base	// common fields
	host1		*string		// list of ports to mirror
	host2		*string		// destination of mirrors
	//protocol	*string		//
	tpport1		*string		//
	tpport2		*string		// these match h1/h2 respectively
	//bandw_in	int64		// bandwidth to reserve inbound to host1
	//bandw_out	int64		// bandwidth to reserve outbound from host1
	//dscp		int			// dscp value that should be propagated
	//dscp_koe	bool		// true if the dscp value should be kept when a packet exits the environment
	qid			*string		// physical host
	//path_list	[]*Path		// list of paths that represent the bandwith and can be used to send flowmods etc.

	//mbox_list	[]*Mbox		// list of middleboxes if the pledge is a steering pledge
	//mbidx		int			// insertion point into mblist
	match_v6	bool		// true if we should force flow-mods to match on IPv6
	tenant_id	*string
	options		*string

	stdout		[]string	// stdout/err from last remote command -- not saved in checkpoints!
	stderr		[]string
}

/*
	A work struct used to decode a json string using Go's json package which requires things to
	be exported (boo). We need this to easily parse the json saved in the checkpoint file.
	We assume that host1/2 are saved _with_ trailing :port and thus we don't explicitly save/restore
	the tp port fields.  The conversion from checkpoint value to full struct will split them off.
*/
type Json_pledge struct {
	Host1		*string
	Host2		*string
	Protocol	*string
	Commence	int64
	Expiry		int64
	Bandwin		int64
	Bandwout	int64
	//Dscp		int
	//Dscp_koe	bool
	Id			*string
	Qid			*string
	Usrkey		*string
	Ptype		int
	//Mbox_list	[]*Mbox
	Match_v6	bool
	Tenant_id	*string
	Options		*string
}

// ---- private -------------------------------------------------------------------

// ---- public -------------------------------------------------------------------

/*
 *	Makes a mirroring pledge.
 */
func Mk_mirror_pledge( in_ports []string, out_port *string, commence int64, expiry int64, id *string, usrkey *string, phost *string, vlan *string, tenant *string, opts *string ) ( p Pledge, err error ) {
	err = nil

	window, err := mk_pledge_window( commence, expiry )		// will adjust commence forward to now if needed, returns nil if expiry has past
	if err != nil {
		return
	}

	t := strings.Join(in_ports, " ")
	if vlan != nil && *vlan != "" {
		// Since we have to cram this in the pre-existing Pledge struct,
		// just glom it on the end of the port list
		// 2015/05/26... Now can redo it to make more sense.
		t = t + " vlan:" + *vlan
	}
	pm := &Pledge_mirror {
		Pledge_base:Pledge_base{
			id: id,
			usrkey: usrkey,			// user "cookie"
			window: window,
		},
		host1:		&t,				// mirror input ports (space sep)
		host2:		out_port,		// mirror output port
		qid:		phost,			// physical host (overloaded field)
		tenant_id:	tenant,
		options:	opts,
		stdout:		make([]string, 0),
		stderr:		make([]string, 0),
	}

	if *usrkey == "" {
		pm.usrkey = &empty_str
	}

	p = pm
	return
}

/*
	Create a clone of the pledge.  The path is NOT a copy, but just a reference to the list
	from the original.
*/
func (p *Pledge_mirror) Clone( name string ) ( Pledge ) {
	newp := &Pledge_mirror {
		Pledge_base:Pledge_base{
			id:			p.id,
			usrkey:		p.usrkey,			// user "cookie"
			pushed:		p.pushed,
			paused:		p.paused,
		},
		host1:		p.host1,
		host2:		p.host2,
		//tpport1: 	p.tpport1,
		//tpport2: 	p.tpport2,
		//bandw_in:	p.bandw_in,
		//bandw_out:	p.bandw_out,
		//dscp:		p.dscp,
		qid:		p.qid,
		//path_list:	p.path_list,
		tenant_id:	p.tenant_id,
		options:	p.options,
		stdout:		make([]string, 0),
		stderr:		make([]string, 0),
	}

	newp.window = p.window.clone()
	return newp
}

/*
	Destruction
*/
func (p *Pledge_mirror) Nuke( ) {
	p.host1 = nil
	p.host2 = nil
	p.id = nil
	p.qid = nil
	p.usrkey = nil
}

/*
	Given a json string unpack it and put it into a pledge struct.
*/
func (p *Pledge_mirror) From_json( jstr *string ) ( err error ){
	jp := new( Json_pledge )
	err = json.Unmarshal( []byte( *jstr ), &jp )
	if err != nil {
		return
	}

	if jp.Ptype != PT_MIRRORING {
		err = fmt.Errorf( "json was not for a mirror pledge" )
		return
	}

	p.host1, p.tpport1 = Split_port( jp.Host1 )		// suss apart host and port
	p.host2, p.tpport2 = Split_port( jp.Host2 )

	p.window, _ = mk_pledge_window( jp.Commence, jp.Expiry )
	//p.protocol = jp.Protocol
	p.id = jp.Id
	//p.dscp_koe = jp.Dscp_koe
	p.usrkey = jp.Usrkey
	p.qid = jp.Qid
	p.tenant_id = jp.Tenant_id
	p.options = jp.Options
	//p.bandw_out = jp.Bandwout
	//p.bandw_in = jp.Bandwin

	return
}

/*
	Associates a queue ID with the pledge.
func (p *Pledge_mirror) Set_qid( id *string ) {
	p.qid = id
}
*/

/*
	Set match v6 flag based on user input.
*/
func (p *Pledge_mirror) Set_matchv6( state bool ) {
	p.match_v6 = state
}

/*
	Accepts a physical host name and returns true if it matches either of the names associated with
	this pledge.
*/
func (p *Pledge_mirror) Has_host( hname *string ) ( bool ) {
	return *p.qid == *hname
}

/*
	must implement dummy for interface
func (p *Pledge_mirror) Set_path_list( pl []*Path ) {
	return
}
*/


/*
	Add a protocol reference to the pledge (e.g. tcp:80 or udp:4444)
func (p *Pledge_mirror) Add_proto( proto *string ) {
	if p == nil {
		return
	}

	p.protocol = proto
}
*/

/*
	Return the protocol associated with the pledge.
func (p *Pledge_mirror) Get_proto( ) ( *string ) {
	return p.protocol
}
*/

func (p *Pledge_mirror) Get_Tenant() *string {
	return p.tenant_id
}

func (p *Pledge_mirror) Set_Output( stdout []string, stderr []string ) {
	p.stdout = stdout
	p.stderr = stderr
}

func (p *Pledge_mirror) Get_Output() ( []string, []string ) {
	return p.stdout, p.stderr
}

func (p *Pledge_mirror) Get_Options() ( *string ) {
	return p.options
}

// --------- humanisation or export functions --------------------------------------------------------

/*
	return a nice string from the data.
*/
func (p *Pledge_mirror) To_str( ) ( s string ) {
	return p.String()
}

func (p *Pledge_mirror) String( ) ( s string ) {

	state, caption, diff := p.window.state_str( )
	c, e := p.window.get_values( )

	//NEVER put the usrkey into the string!
	s = fmt.Sprintf( "%s: togo=%ds %s ports=%s output=%s id=%s st=%d ex=%d push=%v ptype=mirroring", state, diff, caption,
		*p.host1, *p.host2, *p.id, c, e, p.pushed )

	return
}

/*
	Generate a json representation of a pledge. We do NOT use the json package because we
	don't put the object directly in; we render useful information, which excludes some of
	the raw data, and we don't want to have to expose the fields publicly that do go into
	the json output.
*/
func (p *Pledge_mirror) To_json( ) ( json string ) {

	state, _, diff := p.window.state_str( )

	json = fmt.Sprintf( `{ "state": %q, "time": %d, "host1": "%s", "host2": "%s", "id": %q, "tenant_id": %q, "options": %q, "ptype": %d }`,
		state, diff, *p.host1, *p.host2, *p.id, *p.tenant_id, *p.options, PT_MIRRORING )

	return
}

/*
	Build a checkpoint string -- probably json, but it will contain everything including the user key.
	We still won't use the json package because that means making all of the fields available to outside
	users.

	There is no path information saved in the checkpt. If a reload from ckpt is needed, then we assume
	that the network information was completely reset and the paths will be rebuilt using the host,
	commence, expiry and bandwidth information that was saved.

	If the pledge is expired, the string "expired" is returned which seems a bit better than just returning
	an empty string, or "{ }" which is meaningless.
*/
func (p *Pledge_mirror) To_chkpt( ) ( chkpt string ) {

	if p.window.is_expired( ) {			// will show expired if window is nil, so safe without check
		chkpt = "expired"
		return
	}

	c, e := p.window.get_values( )

	options := ""
	if p.options != nil {
		options = *p.options
	}

	tenant_id := "unknown"
	if p.tenant_id != nil {
		tenant_id = *p.tenant_id
	} 

	chkpt = fmt.Sprintf(
		`{ "host1": "%s", "host2": "%s", "commence": %d, "expiry": %d, "id": %q, "qid": %q, "usrkey": %q, "tenant_id": %q, "options": %q, "ptype": %d }`,
		*p.host1, *p.host2, c, e, *p.id, *p.qid, *p.usrkey, tenant_id, options, PT_MIRRORING )

	return
}

/*
	Returns true if kind is PT_MIRRORING, false otherwise
*/
func (p *Pledge_mirror) Is_ptype( kind int ) ( bool ) {
	return kind == PT_MIRRORING
}

/*
	Return the type of pledge; one of the PT_ constants.
func (p *Pledge_mirror) Get_ptype( ) ( int ) {
	return PT_MIRRORING
}
*/

/*
	Return whether the match on IPv6 flag is true
*/
func (p *Pledge_mirror) Get_matchv6() ( bool ) {
	return p.match_v6
}

/*
	Returns a pointer to the queue ID
*/
func (p *Pledge_mirror) Get_qid( ) ( *string ) {
	if p == nil {
		return nil
	}

	return p.qid
}

/*
	Returns pointers to both host strings that comprise the pledge.
*/
func (p *Pledge_mirror) Get_hosts( ) ( *string, *string ) {
	if p == nil {
		return &empty_str, &empty_str
	}

	return p.host1, p.host2
}

/*
	Returns the set of values that are needed to create a pledge in the network:
		pointer to host1 name,
		pointer to host2 name,
		the h1 transport port number and mask or ""
		the h2 transport port number and mask or ""
		the commence time,
		the expiry time,
		Bandwidth values (always 0, but written for parm consistency)
*/
func (p *Pledge_mirror) Get_values( ) ( h1 *string, h2 *string, p1 *string, p2 *string, commence int64, expiry int64, bw_in int64, bw_out int64 ) {
	if p == nil {
		return &empty_str, &empty_str, &empty_str, &empty_str, 0, 0, 0, 0
	}

	c, e := p.window.get_values( )
	return p.host1, p.host2, p.tpport1, p.tpport2, c, e, 0, 0
}

/*
	Return true if the pledge passed in duplicates this pledge.
*/
func (p *Pledge_mirror) Equals( p2 *Pledge ) ( bool ) {

	if p == nil {
		return false
	}

	p2m, ok := (*p2).( *Pledge_mirror )			// convert from generic type to specific
	if ok {
		if ! Strings_equal( p.host1, p2m.host1 ) { return false }
		if ! Strings_equal( p.host2, p2m.host2 ) { return false }
		if ! Strings_equal( p.qid, p2m.qid ) { return false }

		if !p.window.overlaps( p2m.window ) {
			return false;
		}

		return true
	}

	return false
}
