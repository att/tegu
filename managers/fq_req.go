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
	Mnemonic:	fq_req.go
	Abstract:	Functions that work directly on fq_req structures.
	Date:		22 August 2014
	Author:		E. Scott Daniels

	Mods:		24 Sep 2014 : Added support for vlan id setting.
				16 Jan 2015 : Support port masks in flow-mods.
				20 Apr 2015 : Correct bug - not passing direction of external IP address to agent.
				01 Sep 2015 : Changed bleat level for bwow debugging message.
*/

package managers

import (
	"fmt"
	"encoding/json"
	"time"

	"github.com/att/tegu/gizmos"
)

/*
	Create a structure that is initialised such that the default is to not actually cause
	a match to be generated and forces output to none.
*/
func Mk_fqreq( id *string )  ( np *Fq_req ) {
	output := "none"							// table 90 fmod does not output the packet
	cookie := 0xedde

	fq_match := &Fq_parms{
		Swport:	-1,				// these defaults will not generate any match criteria
		Dscp:	-1,
		Tpsport: &zero_string,
		Tpdport: &zero_string,
	}

	fq_action := &Fq_parms{		// these defaults will not generate any actions
		Meta:	nil,	
		Swport:	-1,
		Dscp:	-1,
		Tpsport: &zero_string,
		Tpdport: &zero_string,
	}
		
	np = &Fq_req {							// fq-mgr request data
		Id:		id,
		Cookie:	cookie,
		Expiry:	10,					// default to a very short lived f-mod (DON'T defaut to 0)
		Match: 	fq_match,
		Action: fq_action,
		Table:	0,
		Output: &output,			// default to no output
		Dscp_koe: false,
		Ipv6:	false,
	}

	return
}
/*
	Makes a deep copy (copies the match and action structs too) of a fq request
	structure. Returns a pointer to the new struct.
*/
func ( src *Fq_req ) Clone( ) ( nr *Fq_req ) {
	nmatch := &Fq_parms{}		// create new
	naction := &Fq_parms{}
	nespq := &gizmos.Spq{}
	nr = &Fq_req{}

	*nr = *src					// copy into our new struct
	*naction = *src.Action		// copy action and match to new structs
	*nmatch = *src.Match
	if src.Espq != nil {
		*nespq = *src.Espq
	}

	nr.Espq = nespq
	nr.Match = nmatch			// must reset pointers in new to copies of match and action
	nr.Action = naction

	return
}

/*
	Bundle the structure into json.
*/

func ( fq *Fq_req ) To_json( ) ( *string, error ) {
	jbytes, err := json.Marshal( fq )			// bundle into a json string

	s := string( jbytes )

	return &s, err
}

/*
	Build a map suitable for use as parms for a bandwidth request to the agent manager.
	The agent bandwidth flow-mod generator takes a more generic set of parameters
	and the match/action information is "compressed".

	OVS doesn't accept DSCP values, but shifted values (e.g. 46 == 184), so we shift
	the DSCP value given to be what OVS might want as a parameter.
*/
func ( fq *Fq_req ) To_bw_map( ) ( fmap map[string]string ) {
	fmap = make( map[string]string )

	if fq == nil {
		return
	}

	if fq.Match.Smac != nil {
		fmap["smac"] = *fq.Match.Smac
	} else {
		fmap["smac"] = ""
	}
	if fq.Match.Dmac != nil {
		fmap["dmac"] = *fq.Match.Dmac
	} else {
		fmap["dmac"] = ""
	}
	if fq.Extip != nil {
		fmap["extip"] = *fq.Extip												// external IP if supplied
	} else {
		fmap["extip"] = ""
	}
	if fq.Exttyp != nil {
		fmap["extdir"] = *fq.Exttyp												// direction of external address (-D or -S)
	} else {
		fmap["extdir"] = ""
	}
	if fq.Match.Vlan_id != nil {												// adds a vlan number to match (should NOT be a mac)
		fmap["vlan_match"] = *fq.Match.Vlan_id
	} else {
		fmap["vlan_match"] = ""
	}
	if fq.Action.Vlan_id != nil {												// adds a set vlan action, can be a MAC for late conversion
		fmap["vlan_action"] = *fq.Action.Vlan_id
	} else {
		fmap["vlan_action"] = ""
	}

	fmap["queue"] =  fmt.Sprintf( "%d", fq.Espq.Queuenum )
	fmap["dscp"] =  fmt.Sprintf( "%d", fq.Dscp << 2 )						// shift left 2 bits to match what OVS wants
	fmap["ipv6"] =  fmt.Sprintf( "%v", fq.Ipv6 )							// force ipv6 fmods is on
	fmap["timeout"] =  fmt.Sprintf( "%d", fq.Expiry - time.Now().Unix() )
	//fmap["mtbase"] =  fmt.Sprintf( "%d", fq.Mtbase )
	fmap["oneswitch"] = fmt.Sprintf( "%v", fq.Single_switch )
	fmap["koe"] = fmt.Sprintf( "%v", fq.Dscp_koe )
	if fq.Tptype != nil && *fq.Tptype != "none" {
		if fq.Match.Tpsport != nil && *fq.Match.Tpsport != "0" {
			fmap["sproto"] = fmt.Sprintf( "%s:%s", *fq.Tptype, *fq.Match.Tpsport )
		}
		if fq.Match.Tpdport != nil && *fq.Match.Tpdport != "0" {
			fmap["dproto"] = fmt.Sprintf( "%s:%s", *fq.Tptype, *fq.Match.Tpdport )
		}
	}

	if fq_sheep.Would_baa( 3 ) {
		for k, v := range fmap {
			fq_sheep.Baa( 3, "fq_req to action id=%s %s = %s", fq.Id, k, v )
		}
	}

	return
}


/*
	Build a map suitable for use as parms for a oneway bandwidth request to the agent manager.
	The agent bandwidth flow-mod generator takes a more generic set of parameters
	and the match/action information is "compressed".

	OVS doesn't accept DSCP values, but shifted values (e.g. 46 == 184), so we shift
	the DSCP value given to be what OVS might want as a parameter.
*/
func ( fq *Fq_req ) To_bwow_map( ) ( fmap map[string]string ) {
	fmap = make( map[string]string )

	if fq == nil {
		return
	}

	if fq.Match.Smac != nil {
		fmap["smac"] = *fq.Match.Smac
	} else {
		fmap["smac"] = ""
	}
	if fq.Match.Dmac != nil {					// likely nil as oneways are usually x-project when router is not a neutron device
		fmap["dmac"] = *fq.Match.Dmac
	} else {
		fmap["dmac"] = ""
	}
	if fq.Extip != nil {
		fmap["extip"] = *fq.Extip												// external IP if supplied
	} else {
		fmap["extip"] = ""
	}
	if fq.Match.Vlan_id != nil {												// adds a vlan number to match (should NOT be a mac)
		fmap["vlan_match"] = *fq.Match.Vlan_id
	} else {
		fmap["vlan_match"] = ""
	}
	if fq.Action.Vlan_id != nil {												// adds a set vlan action, can be a MAC for late conversion
		fmap["vlan_action"] = *fq.Action.Vlan_id
	} else {
		fmap["vlan_action"] = ""
	}

	fmap["queue"] =  fmt.Sprintf( "%d", fq.Espq.Queuenum )
	fmap["dscp"] =  fmt.Sprintf( "%d", fq.Dscp << 2 )						// shift left 2 bits to match what OVS wants
	fmap["ipv6"] =  fmt.Sprintf( "%v", fq.Ipv6 )							// force ipv6 fmods is on
	fmap["timeout"] =  fmt.Sprintf( "%d", fq.Expiry - time.Now().Unix() )
	if fq.Tptype != nil && *fq.Tptype != "none" {
		if fq.Match.Tpsport != nil && *fq.Match.Tpsport != "0" {
			fmap["sproto"] = fmt.Sprintf( "%s:%s", *fq.Tptype, *fq.Match.Tpsport )
		}
		if fq.Match.Tpdport != nil && *fq.Match.Tpdport != "0" {
			fmap["dproto"] = fmt.Sprintf( "%s:%s", *fq.Tptype, *fq.Match.Tpdport )
		}
	}

	if fq_sheep.Would_baa( 2 ) {
		for k, v := range fmap {
			fq_sheep.Baa( 2, "fq_req to action id=%s %s = %s", *fq.Id, k, v )
		}
	}

	return
}

/*
	Build a map suitable for use as parms for a passthrough request to the agent manager.
			build_opt( parms["smac"], "-s" ) +				// smac can (and should) be an endpoint UUID which is converted to mac/bridge on the host
			build_opt( parms["sip"], "-S" ) +				// sip is [proto:][addr:][port] where either proto or address must be supplied
			build_opt( parms["timeout"],  "-t" )
*/
func ( fq *Fq_req ) To_pt_map( ) ( fmap map[string]string ) {
	fmap = make( map[string]string )

	if fq == nil {
		return
	}

	if fq.Match.Smac != nil {					// could be endpoint, but likely mac in the original Tegu.
		fmap["smac"] = *fq.Match.Smac
	} else {
		fmap["smac"] = ""						// agent likely to barf on this
	}

	fmap["timeout"] =  fmt.Sprintf( "%d", fq.Expiry - time.Now().Unix() )
	fmap["sip"] = *fq.Match.Ip1								// will be [{udp|tcp}:]address[:port]

	if fq_sheep.Would_baa( 3 ) {
		for k, v := range fmap {
			fq_sheep.Baa( 3, "to_pt_map: fq_req value -> action id=%s %s = %s", *fq.Id, k, v )
		}
	}

	return
}
