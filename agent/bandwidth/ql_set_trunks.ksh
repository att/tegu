#!/usr/bin/env ksh
# vi: sw=4 ts=4:
#
# ---------------------------------------------------------------------------
#   Copyright (c) 2013-2015 AT&T Intellectual Property
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at:
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.
# ---------------------------------------------------------------------------
#

#	Mnemonic:	ql_set_trunks
#	Abstract:	Read the output from ovs_sp2uuid and suss off all VLAN IDs for br-int. Then
#				generate a trunk command that adds the trunk list to the qosirl0 interface.
#				Trunks must be set on the interface in order to set the vlan-id in a flow-mod
#				as data passes to the interface. Trunks canNOT be set as a range.
#
#				As of OVS 2.1 it seems that the listing of VLAN IDs on a trunk port isn't
#				needed, so this script may now be deprecated.
#
#				To delete all VLAN IDs from the trunk list:
#					ovs-vsctl remove port 94da17a2-5042-476a-8a49-1e112e273c14 trunks 2,3,7,8,9,17,4095
#
#	Author:		E. Scott Daniels
#	Date:		09 April 2015
#				17 Feb 2016 : Convert over to ql_suss_ovsd and to ensure that vlan 1 always set.
#
#---------------------------------------------------------------------------------------------

forreal=""

while [[ $1 == -* ]]
do
	case $1 in
		-n)		forreal="echo would execute: ";;

		-\?)	echo "usage: $0 [-n]"
				exit 0
				;;

		*)		echo "usage: $0 [-n]"
				exit 1
				;;
	esac

	shift
done

ql_suss_ovsd -a | awk '
	BEGIN {
		seen[1] = 1								# must force 1 in because vlan for rate limit bridges always set to 1
		max = 1
	}

	/^switch:/ {
		snarf = 0
		if( $NF == "br-int" )
			snarf = 1
		next;
	}

	/^port:.*qosirl0[ \t]/ {					# port id for the ovs-vsctl command
		irl_id = $2;
		next;
	}

	/^port:/ && NF > 6 {						# old versions didn't put out all fields, just be safe
		if( $7+0 > 0 ) {
			seen[$7] = 1
			if( $7 > max )
				max = $7
		}
		next;
	}

	END {
		sep = ""
		list = ""
		for( i = 1; i <= max; i++ ) {			# keeps them sorted
			if( seen[i] ) {
				list = list sprintf( "%s%d", sep, i )
				sep = ","
			}
		}

		if( irl_id != "" ) {
			printf( "sudo ovs-vsctl set port %s trunks=%s\n", irl_id, list )
		} else {
			printf( "ERR: unable to find qosirl0 interface in ovs_sp2uuid list\n" ) >"/dev/fd/2"
			exit( 1 )
		}
	}
'| read cmd

if [[ -n $cmd ]]
then
	$forreal $cmd
else
	"ERR no trunk command generated"
	exit 1
fi

exit 0

