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


#	Mnemonic:	ql_suss_ovsd -- suss ovs data
#	Abstract:	This script roots through the vastness of the OVS database and 
#				generates a dump of the useful information which is stored in 
# 				a typical scattered format inside of OVS' db. 
#
#				Output is a record per 'device' where device is either a swtitch (bridge)
#				or port.  First field identifies the type; the remainder of the fields 
#				depend on type.
#					switch: <dpid> <uuid> <name>
#					port: <uuid> <of-port> <ifname> <mac> <openstk-uuid> <vlan> <bridge>
#
#				examples:
#					switch: 0000b8ca3a656ca4 54bf144b-16ea-495c-ae13-c62b8d049aa5 br-ex
#					port: 0fa3550d-d8d8-4756-9a34-39f9c51c6abd 4 em3 . . -1 br-ex
#					port: 5e6a4257-fd76-4cbf-95d1-7320432c8618 6 qg-b83f0a63-68 fa:de:ad:8e:ee:c5 b83f0a63-6888-4480-8947-360c8d896e4a -1 br-ex
#
#				Information that is not available is output as a single dot (.) in the column
#				such that there are no empty columns.
#
#
#				NOTE:
#				Replaces ovs_sp2uuid, and is pretty much the same except that we
#				assume the command will always run locally (no ssh), and that all
#				port output records have the same number of fields. Field labeling
#				is optional for better support of human use.
#
#	Date:		09 October 2015
#	Author:		E. Scott Daniels
#
#	Mods:		13 Nov 2015 - removed some debugging.
# -----------------------------------------------------------------------------------------------

# executes the needed ovs commands which generate all the bits that we need to parse through.
# tags each section with an eye-catcher.
#
function run_ovs_cmds
{
	set -e
	echo "BRIDGEDATA"
	$sudo ovs-vsctl list Bridge
	echo "PORTDATA"
	$sudo ovs-vsctl list Port
	echo "IFACEDATA"
	$sudo ovs-vsctl list Interface
	set +e
}

# pass the stdin through ql_filter_router to remove any routers that show in ovs' db
# but aren't actually present on the node (openstack seems not to remove them when it
# moves them or somesuch).  "Filtering" can be turned off with -f on commandline.
# ql_filter_rtr expects it on stdin.
#
function strip_routers
{
	if (( filter ))
	then
		ql_filter_rtr				# lop off routers that OVS reports, but aren't actually here
	else
		cat							# useful use of cat :)
	fi
}

# --------------------------------------------------------------------------------------------------

filter=1					# -f can set to 0 to disable the router filter
label=0
ssh_host=""
label=0
rhost="localhost"
drop_internal=0
ssh_opts="-o ConnectTimeout=2 -o StrictHostKeyChecking=no -o PreferredAuthentications=publickey"

while [[ $1 == -* ]]
do
	case $1 in
		-a)	;;					# back compat with old ovs_sp2uuid command
		-d) drop_internal=1;;
		-f)	filter=0;;
		-l)	label=1;;

		*)	
			echo "usage: $0 [-d] [-f] [-l]"
			exit 1
			;;
	esac

	shift
done


if (( $(id -u) != 0 ))
then
	sudo="sudo"					# must use sudo for the ovs-vsctl commands
fi

data=/tmp/PID$$.data
run_ovs_cmds >$data				# must do this because things hang if sudo requires password and we are piping through filter
awk \
	-v drop_internal=$drop_internal \
	-v desport=${2:--1} \
	-v label=${label:-0} \
	'
	BEGIN {
	}
	/ERROR!/ { exit( 1 ) }

	/BRIDGEDATA/ { bridge_type = 1; next; }
	/IFACEDATA/ { bridge_type = 0; next; }
	/PORTDATA/ { bridge_type = 0; next; }
	
	#external_ids        : {attached-mac="fa:de:ad:43:a3:0c", iface-id="75d11a94-8042-4a6e-8261-5fc835d67a71", iface-status=active}
	/^external_ids/ {							# pull the id a and mac that openstack gave to the interface
		exmac[id] = "."
		exifaceid[id] = "."

		gsub( "[][,{}\"]", "" );
		for( i = 3; i <= NF; i++ )
		{
			if( split( $(i), a, "=" ) == 2 )
			{
				if( a[1] == "attached-mac" )
					exmac[id] = a[2];
				else
				if( a[1] == "iface-id" )
					exifaceid[id] = a[2];
			}
		}
		next;
	}

	/^name/ {
		gsub( "\"", "" );
		gsub( "}", "" );
		gsub( "{", "" );
		name = $NF;
		id2name[id] = $NF;

		nports[id] = 0
		ofname[id] = "."
		ofport[id] = -1
		next;
	}

	/^_uuid/ {
		gsub( "\"", "" );
		gsub( "}", "" );
		gsub( "{", "" );
		id = $NF;
		if( bridge_type )
			seen[id] = 1;
	}

	/^ports/ {
		gsub( "[][,]", "" );
		for( i = 3; i <= NF; i++ )
			ports[id,i-3] = $(i);
		nports[id] = (i-3)+1;
		next;
	}

	/^ofport_request/ { next; }		
	/^ofport/ {
		ofport[id] = $NF;
		ofname[id] = name;
		next;
	}

	/^datapath_id/ {
		gsub( "\"", "" );
		dpid[id] = $NF;
		dpid2uuid[$NF] = id;
		next;
	}

	/^interfaces/ {
		gsub( "[][,]", "" );
		for( i = 3; i <= NF; i++ )
			iface[id,i-3] = $(i);
		niface[id] = (i-3)+1;
		next;
	}

	/^tag/  {
		if( $NF + 0 > 0 )
			vlan_tag[id] = $NF + 0;
		else
			vlan_tag[id] = -1;
		next;
	}

	/^type.*internal/ {
		if( drop_internal && !bridge_type )
			drop_if[id] = 1
		next;
	}

	END {
		for( id in seen )				# for each switch
		{
			if( label )
				printf( "switch: dpid=%s uuid=%s name=%s\n", dpid[id], id, id2name[id] )
			else
				printf( "switch: %s %s %s\n", dpid[id], id, id2name[id] )

			for( i = 0; i < nports[id]; i++ )
			{
				pid = ports[id,i];	
				if( pid != ""  &&  ofport[iface[pid,0]] != "" )
					if( ! drop_if[iface[pid,0]] ) {
						if( label )
							printf( "port: uuid=%s of_portn=%s of_name=%s mac=%s neutron_uuid=%s vlan=%d br=%s\n", pid, ofport[iface[pid,0]], ofname[iface[pid,0]], exmac[iface[pid,0]], exifaceid[iface[pid,0]], vlan_tag[pid], id2name[id] );
						else
							printf( "port: %s %s %s %s %s %d %s\n", pid, ofport[iface[pid,0]], ofname[iface[pid,0]], exmac[iface[pid,0]], exifaceid[iface[pid,0]], vlan_tag[pid], id2name[id]);
					}
			}
		}
	}

' $data | strip_routers
exit $?

