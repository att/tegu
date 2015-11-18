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

	Mnemonic:	lite
	Abstract:	Functions specifically added to support qos-lite.
				These should later be broken into better organised files, but since
				this is a deathmarch they are all stuck in here to make it easy (but
				messy).
	Date:		28 April 2014
	Author:		E. Scott Daniels
				29 Jul 2014 : Mlag support

*/

package gizmos

import (
	//"bufio"
	"encoding/json"
	"fmt"
	"os"
	//"strings"
	//"time"
)


/*
	Reads the file which is assumed to contain nothing but the json link
	in floodlight syntax.
*/
func Read_json_links( fname *string ) ( links []FL_link_json, err error ) {

	if fname == nil {
		return nil, fmt.Errorf( "no filename for static topo" )
	}

    f, err := os.Open( *fname )
	links = nil

    if err != nil {
        return;
    }
    defer f.Close()


	links = make( []FL_link_json, 0 )
	jdecoder := json.NewDecoder( f )
	err = jdecoder.Decode( &links )

	//TODO:	parse the list of links and create 'internal' linkes e.g. br-em1...br-int and br-em2...br-int
	// for now we strip @interface name from the switch id
/*
	for i := range links {
		n := strings.Index( links[i].Dst_switch, "@" )
		if  n >= 0 {											// if this is indicates the interface name
			links[i].Dst_switch = links[i].Dst_switch[0:n]		// ditch it for now
		}
	}
*/

	return
}


/*
	Request vm information from openstack and generate the 'host json' that is a
	match for the floodlight dev api output:
		dev[0]:
    		entityClass = DefaultEntityClass
    		mac[0] = fa:de:ad:a9:9d:c5
    		ipv4[0] = 10.67.0.4
    		attachmentPoint[0]:
        		switchDPID = 00:00:d2:56:96:3f:7d:46
        		port = 113.00
        		errorStatus = null/undefined
    		lastSeen = 1398705932064.00


	This must be a part of network manger because the net struct is where all the maps are and it's
	just easier to keep it there.
*/
	
