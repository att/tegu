#!/usr/bin/env ksh
# vi: sw=4 ts=4:

#	Mnemonic:	ovs_sp2uuid -- map a switch dpid/port combination to ovs UUID.
#	Abstract:	This script accepts the dpid for a switch and an openflow port 
#				number and writes the UUID of the asscoiated internal OVS port
#				that is needed when managing queues.  The UUID can then be given on 
#				an an ovs command to set the qos queues (e.g.
#					ovs-vsctl set Port <uuid> qos=<qos-name>
#
#				where <uuid> is the value returned by this script and <qos-name> is
#				is the uuid of the qos entry that should be assigned to the port.
#
#				If -k file is given to the script the ovs data needed to suss out the
#				answer is saved in file and that can be supplied on future calls to 
#				this script (-d file) to avoid repeated queries against the ovs 
#				environment.
#
#				To allow our stuff to run on a centralised host, where  the remote host consists 
#				only of a user id and 	none of our code,  we bundle ovs-* commands into as large
#				of a batch as is possible and submit them in one ssh session as session setup is 
#				painfully expensive. 
#
#	Date:		05 February 2014
#	Author:		E. Scott Daniels
#
#	Mods:		03 Mar 2014 - Added sudo when needed to the ovs-vsctl calls
#				07 Mar 2014 - Corrected bug with default name.
#				20 Mar 2014 - Added -a option which shows additional information for each 
#					switch and port. Fixed bug that was causing some info to be missed 
#					on some systems.
#				23 Apr 2014 - Hacked to support running the commands on a remote host.
#				27 Apr 2014 - Added support to generate external mac and id when -a is given.
#				04 May 2014 - Added some error checking and reporting. 
#				13 May 2014 - Added ssh options to prevent prompts when new host tried
#				24 Sep 2014 - Now captures vlan-id and puts that out with additional info
#				10 Nov 2014 - Added connect timeout to ssh calls
#				17 Nov 2014	- Added timeouts on ssh commands to prevent "stalls" as were observed in pdk1.
#				05 Dec 2014 - Default to dropping interfaces marked as internal
#				10 Dec 2014 - Reert the default to dropping interfaces marked as internal as some 
#					gateways (router) are marked by quantum as internal.
#				28 Dec 2015 - Prevent actually using ssh if the host given with -h is the localhost.
#				14 Apr 2015 - Added call to filter_rtr which should eliminate any Openstack routers
#					that are left in the OVS database, but aren't actually on the host.
#					NOTE: filter router can be used ONLY if this script is executing on the local
#					host and not if it's sending ovs commands to a remote host.
# -----------------------------------------------------------------------------------------------

# echos out the ovs commands that are needed to run in an ssh on the remote system
# (allows for easy retry)
function cat_ovs_cmds
{
		cat <<endKat 
			echo "BRIDGEDATA"
			$sudo ovs-vsctl list Bridge 
			echo "PORTDATA"
			$sudo ovs-vsctl list Port 
			echo "IFACEDATA"
			$sudo ovs-vsctl list Interface 
endKat
}

# pass the standard in through the filter_router unless we are running this command on behalf
# of another host. We assume the output from this script is being piped to this function
# as ql_filter_rtr expects it on stdin.
function filter
{
	if (( filter )) && [[ -z $ssh_host ]]		# cannot filter if target host is not local
	then
		ql_filter_rtr				# lop off routers that OVS reports, but aren't actually here
	else
		cat							# useful use of cat :)
	fi
}

# --------------------------------------------------------------------------------------------------

keep="/dev/null"
filter=1					# -f can set to 0 to disable the filter
show_adtl=0
ssh_host=""
rhost="localhost"
drop_internal=0
ssh_opts="-o ConnectTimeout=2 -o StrictHostKeyChecking=no -o PreferredAuthentications=publickey"

while [[ $1 == -* ]]
do
	case $1 in 
		-a)	show_adtl=1;;
		-d)	data=$2; shift;;
		-f)	filter=0;;
		-h)	
			if [[ $2 != $(hostname) && $2 != "localhost" ]]
			then
				ssh_host="ssh $ssh_opts $2"; 		# host where we'll run the ssh commands
				rhost=$2
			fi
			shift
			;;		
		-k)	keep=$2; shift;;
		-K)	drop_internal=0;;

		*)	
			cat <<-endKat
			"usage: $0 [-a] [-h host] [-k keep-data-file] [-d ovs-data] switch-dpid port"
			where
				-a lists all information, not just port and uuid
				-h supplies the host where the ovs data is gatherd
				-k keeps the data file for reuse
				-d supplies the data file to use from a previous execution

				The word "all" can be used for the switch id to cause all switches to be listed. 
			endKat
			exit 1
	esac

	shift
done


if (( $(id -u) != 0 ))
then
	sudo="sudo"					# must use sudo for the ovs-vsctl commands
fi

(
	if [[ -z $data ]]
	then
		cat_ovs_cmds | timeout 15 $ssh_host ksh 		# bundle and execute all commands in one session
		rc=$?
		if (( rc > 100 ))								# timeout should pop with 124
		then
			echo "retrying ssh to $rhost; ssh timeout detected" >&2
			cat_ovs_cmds | timeout 15 $ssh_host ksh 		# bundle and execute all commands in one session
			rc=$?
		fi

		if (( rc != 0 ))
		then
			echo "ERROR!"
		fi
	else
		cat $data
	fi
) | tee $keep |  awk \
	-v drop_internal=$drop_internal \
	-v show_adtl=$show_adtl \
	-v desid=${1:-any} \
	-v desport=${2:--1} \
	'
	BEGIN {
		gsub( ":", "", desid );
	}
	/ERROR!/ { exit( 1 ) }

	/BRIDGEDATA/ { bridge_type = 1; next; }
	/IFACEDATA/ { bridge_type = 0; next; }
	/PORTDATA/ { bridge_type = 0; next; }
	
	#external_ids        : {attached-mac="fa:de:ad:43:a3:0c", iface-id="75d11a94-8042-4a6e-8261-5fc835d67a71", iface-status=active}
	/^external_ids/ {							# pull the id a and mac that openstack gave to the interface
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
		if( desid != "any" )								# find specific switch/port
		{
			id = dpid2uuid[desid]; 	# get switch uuid
			for( i = 0; i < nports[id]; i++ )
			{
				pid = ports[id,i];	
				for( j = 0; j < niface[pid]; j++ )				# each port can have multiple interfaces??
					if( desport < 0 || ofport[iface[pid,j]] == desport )		# if this interface matches the ofport we desire
					{
						if( show_adtl ) 			#  expanded output: print the _port_ uuid as that is needed to set a queue
							printf( "%s  %s %s %s %s\n", pid, ofname[iface[pid,0]], exmac[iface[pid,0]], exifaceid[iface[pid,0]], vlan_tag[pid] );	
						else
							printf( "%s\n", pid );					# then print the _port_ uuid as that is needed to set a queue
						exit( 0 );
					}
			}
				
			exit( 1 );
		}

		for( id in seen )
		{
			if( show_adtl )
				printf( "switch: %s %s %s\n", dpid[id], id, id2name[id] )
			else
				printf( "switch: %s %s\n", dpid[id], id )
			for( i = 0; i < nports[id]; i++ )
			{
				pid = ports[id,i];	
				if( pid != ""  &&  ofport[iface[pid,0]] != "" )
					if( ! drop_if[iface[pid,0]] ) {
						if( show_adtl )
							printf( "port: %s %s %s %s %s %d\n", pid, ofport[iface[pid,0]], ofname[iface[pid,0]], exmac[iface[pid,0]], exifaceid[iface[pid,0]], vlan_tag[pid] );
						else
							printf( "port: %s %s\n", pid, ofport[iface[pid,0]] );
					}
			}
		}
	}

' | filter
exit $?

