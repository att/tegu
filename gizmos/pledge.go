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

	Mnemonic:	pledge interface
	Abstract:	Defines what constitutes a pledge interface.
				Implemented by pledge_bw, pledge_mirror, pledge_steer and
				maybe others.

				Functions defined by the interface should make sense for ALL
				pledge types.  If they don't then the type(s) that require
				them should implement them and the user will need to convert
				the more generic interface type to the speciic type to invoke
				the function needed. Examples of this which have been
				specifically omitted: Get_values(), Clone(), Set_path_list.

				There are also some generic functions such as json2pledge().
	Date:		21 May 2015
	Author:		E.
Scott Daniels

	Mods:
*/

package gizmos

import (
	"fmt"
	"encoding/json"
)

type Pledge interface {
	Concluded_recently( window int64 ) ( bool )
	Commenced_recently( window int64 ) ( bool )
	Equals( *Pledge ) ( bool )
	Get_hosts() ( *string, *string )
	Get_id( ) ( *string )
	Get_window( ) ( int64, int64 )
	Has_host( *string ) ( bool )
	Is_active( ) ( bool )
	Is_active_soon( window int64 ) ( bool )
	Is_expired( ) ( bool )
	Is_extinct( window int64 ) ( bool )
	Is_pending( ) ( bool )
	Is_pushed( ) (bool)
	Is_paused( ) ( bool )
	Is_valid_cookie( c *string ) ( bool )
	Nuke()
	Pause( bool )
	Reset_pushed( )
	Resume( bool )
	Set_expiry( expiry int64 )
	//Set_matchv6( bool )
	Set_pushed()
	String() ( string )
	To_chkpt( ) ( string )
	To_json( ) ( string )
	To_str() ( string )

	//Get_ptype( ) ( int )		users should use assertion or type determination in switch for these
	//Is_ptype( kind int ) ( bool )					// kind is one of the PT constants
}

// generic struct to unpack any type of pledge in order to determine the type
// This must only contain fields that exist in all pledge types, and only
// the fields that are needed to determine the type.
type J2p struct {
	Ptype	*int
}

/*
	Given a string that contains valid json, unpack it and examine
	the ptype. Based on ptype, allocate a specific pledge block and
	invoke it's function to unpack the string.
*/
func Json2pledge( jstr *string ) ( p *Pledge, err error ) {
	var pi Pledge

	jp := new( J2p )
	err = json.Unmarshal( []byte( *jstr ), &jp )
	if err == nil {
		if jp.Ptype != nil {
			switch *jp.Ptype {
				case PT_BANDWIDTH:
					bp := new( Pledge_bw )
					bp.From_json( jstr )
					pi = Pledge( bp )			// convert to interface type

				case PT_OWBANDWIDTH:			// one way bandwidth
					obp := new( Pledge_bwow )
					obp.From_json( jstr)
					pi = Pledge( obp )
	
				case PT_MIRRORING:
					mp := new( Pledge_mirror )
					mp.From_json( jstr )
					pi = Pledge( mp )			// convert to interface type
					
	
				case PT_STEERING:
					mp := new( Pledge_steer )
					mp.From_json( jstr )
					pi = Pledge( mp )			// convert to interface type
	
				default:
					err = fmt.Errorf( "unknown pledge type in json: %d: %s", *jp.Ptype, *jstr )
					return
			}
		} else {
			err = fmt.Errorf( "no ptype found in json, unable to convert to pledge: %s", *jstr )
		}
	}

	p = &pi
	return
}
