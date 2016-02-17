#!/usr/bin/env bash 
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

# -----------------------------------------------------------------------------------------------------------------
#	Mnemonic:	ql_suss_queues.ksh
#	Abstract:	Dumps several 'tables' from the ovs database and builds a
#				summary of queue information.
#	Author:		E. Scott Daniels
#	Date:		08 September 2014
#
#	Mods:		26 Dec 2013 - Added support for finding and printing queue priority and eliminated the
#					need for user to run under sudu (we'll detect that need and do it).
#				08 Sep 2014 - Ported from the original quick and dirty script (suss_ovs_queues) to make
#					it usable by the tegu agents in a q-lite environment.
#				10 Nov 2014 - Added connect timeout to ssh calls
#				28 Jan 2015 - Changes to eliminate uneeded ssh call if -h parm is for this host or localhost
# -----------------------------------------------------------------------------------------------------------------


trap "cleanup" EXIT

# ensure all tmp files are gone on exit
function cleanup
{
	rm -f /tmp/PID$$.*
}

if (( $( id -u ) != 0 ))
then
	sudo="sudo"
fi

ssh_opts="-o ConnectTimeout=2 -o StrictHostKeyChecking=no -o PreferredAuthentications=publickey"
ssh=""						# if -h given, this gets populated with the ssh command needed to run this on the remote

backlevel_ovs=0					# assume modren ovs (1.10+)
bridge=""
show_headers=1
show_units=1

while [[ $1 == "-"* ]]
do
	case $1 in
		-b)	backlevel_ovs=1;;
		-B)	show_headers=0; bridge=$2; shift;;
		-h)	
			if [[ $2 != $(hostname)  && $2 != "localhost" ]]
			then
				ssh="ssh -n $ssh_opts $2" 		# CAUTION: this MUST have -n since we don't redirect stdin to ssh
			fi
			shift
			;;

		-u)		show_units=0;;

		-\?)	usage;
				exit;;
	esac

	shift
done

# we make a blind assumption that data is presented in the order requested by the --column flag
# grrr.... bleeding ovs-vsctl 1.4.x is broken and doesn't recognise --column even though the man page says otherwise.
(
	if (( ! backlevel_ovs ))
	then
		$ssh $sudo ovs-vsctl --data=bare --column=_uuid,name,port list Bridge $bridge | sed 's/^/BRIDGE: /'
		$ssh $sudo ovs-vsctl --data=bare --column=_uuid,name,qos list Port | sed 's/^/PORT: /'
		$ssh $sudo ovs-vsctl --data=bare --column=_uuid,queues,other_config list QoS | sed 's/^/QOS: /'
		$ssh $sudo ovs-vsctl --data=bare list Queue | sed 's/^/QUEUE: /'
	else

		# turning off columns we can only hope that _uuid is always output first (another claim by the man page, but we will see)
		$ssh $sudo ovs-vsctl --data=bare list Bridge $bridge | sed 's/^/BRIDGE: /'
		$ssh $sudo ovs-vsctl --data=bare list Port | sed 's/^/PORT: /'
		$ssh $sudo ovs-vsctl --data=bare list QoS | sed 's/^/QOS: /'
		$ssh $sudo ovs-vsctl --data=bare list Queue | sed 's/^/QUEUE: /'
	fi
) | awk \
	-v show_units=$show_units \
	-v show_headers=$show_headers \
	'
	BEGIN { seen_idx = 0; }
	NF < 4 { next; }

	/^BRIDGE: name/	{ bname = $NF; seen[seen_idx++] = bname; next; }
	/^BRIDGE: ports/	{
		pidx[bname] = 0;
		for( i = 4; i <= NF; i++ )
		{
			ports[bname,pidx[bname]] = $(i);
			pidx[bname]++;
		}
		next;	
	}


						# CAUTION -- uuid is (should) always be printed first; we dont assume anything about the order of the rest
	/^PORT: _uuid/ {
						puname = $NF;
						pname = "unknown";
						p2qos[puname] = "";
						next;
					}
	/^PORT: name/	{ pname = $NF; u2pname[puname] = pname; next; }	# map the port uuid to human name
	/^PORT: qos.*:/	{ p2qos[puname] = $NF; next; }			# map port to the qos entry

	/^QOS: _uuid/	{ qoname = $NF; next; }
	/^QOS: other_config/	{
		for( i = 4; i <= NF; i++ )
		{
			if( split( $(i), a, "=" ) == 2 )
				qos_cfg[qoname,a[1]] = a[2]
		}
		next;
	}
	/^QOS: queues/	{
		for( i = 4; i <= NF; i++ )
		{
			maxqueue[qoname] = 0;
			if( split( $(i), a, "=" ) == 2 )
			{
				qos_q[qoname,a[1]+0] = a[2]
				if( maxqueue[qoname] < a[1] + 0 )
					maxqueue[qoname] = a[1] + 0;
			}
		}
		next;
	}

	/^QUEUE: _uuid/	{ qname = $NF; next; }
	/^QUEUE: other_config/	{
		for( i = 4; i <= NF; i++ )
		{
			if( split( $(i), a, "=" ) == 2 )		
				qcfg[qname,a[1]] = a[2]
		}
		next;
	}

	END {
		for( i = 0; i < seen_idx; i++ )
		{
			if( show_headers )
				printf( "%s%s\n", i > 0 ? "\n" : "", seen[i] );
			bname = seen[i];
			
			for( j = 0; j < pidx[bname]; j++ )
			{
				pu = ports[bname,j];						# ports id
				qos = p2qos[pu];
				if( qos  != "" )								# qos entry that it maps to
				{
					printf( "  %s\n", u2pname[pu] );				# human port name	
						
					for( k = 0; k <= maxqueue[qos]; k++ )
					{
						qid = qos_q[qos,k];
						if( qid != "" )
						{
							if( show_units )
								printf( "      Q%d: min:%9.3fMbit/s max:%9.3fMbit/s pri:%d\n", k, qcfg[qid,"min-rate"]/1000000, qcfg[qid,"max-rate"]/1000000, qcfg[qid,"priority"] );
							else
							{
								printf( "      Q%d: min: %9.3f max: %9.3f pri: %d\n", k, qcfg[qid,"min-rate"], qcfg[qid,"max-rate"], qcfg[qid,"priority"] );
							}
						}
					}
				}
			}
		}
	}
'

exit $?


# sample ovs output
#bridge:
#name                : s5
#_uuid               : d807cfc0-8cc6-41cb-ac1c-6eb1bd1b6f08
#ports               : 6b200e3a-cfa1-4168-9510-879ee789bc0c 929dc121-d271-4657-8c9a-278712742589 9cb5e906-02d3-4819-9ece-6c8e949a857d c32ec7a5-eb4b-4484-86e5-6ea7f527b969
#
#
#port:
#name                : s2-eth2
#_uuid               : 4a301fb0-df46-4739-8b53-b1f4a46f927c
#qos                 : 414f8f52-3632-4028-a104-78a364bcb0c9
#
#
#qos
#_uuid               : 7211e540-52c1-494f-9cd2-4e90a3307390
#other_config        : max-rate=1000000000
#queues              : 0=cc54fd8c-ad64-43a3-88dd-d01f1fac1c4b 1=9e59eafe-3115-4a73-9340-836b02e632f0 2=d983a9cb-7802-4765-a053-689a55bce270
#
#_uuid               : c56ac7c0-8f2e-4f26-8ebf-9a1a985eca2d
#other_config        : max-rate=1000 min-rate=100
