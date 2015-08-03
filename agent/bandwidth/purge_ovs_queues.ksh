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

#	Mnemonic:	purge_ovs_queues
#	Abstract:	Run though the output of serveral ovs commands looking for QoS entries that
#				are not referenced by any ports. For the ones that are not referenced, we'll
#				generate a list of associated queues that are also not referenced and delete
#				all of the unreferenced things.
#
#	Author:		E. Scott Daniels
#	Date: 		24 February 2014
#
#	Mods:		23 Apr 2014 - Hacks to allow this to be run centrally.
#				13 May 2014 - Now tracks and purges queues that are 'orphaned'; we purge all
#					queues that are unreferenced, not just those that had qos references when
#					the script started.  Purge all still needed as that purges even queues
#					with references.
#				13 May 2014 - Added ssh options to prevent prompts when new host tried
#				10 Nov 2014 - Added connect timeout to ssh calls
#				17 Nov 2014	- Added timeouts on ssh commands to prevent "stalls" as were observed in pdk1.
#				04 Dec 2014 - Ensured that all crit/warn messages have a constant target host component.
#				28 Jan 2014 - To allow agent with ssh-broker to execute on a remote host.
#				16 Mar 2015 - Corrected bug in -h arg processing.
# ----------------------------------------------------------------------------------------------------------
#


function usage
{
	cat <<-endKat
	
	version 1.1/14244
	usage: $argv0 [-a] [-n] [-h host] [-v]

	Removes all individual queues and queue combinations (QoSes in OVS terms) from the local OVS environment.
	Using -n and -v will indicate at various levels of verbosity what would be done rather than actually
	taking the action.

	  -a purge all regardless of reference counts
	  -h host  susses queue information from the named host and purges things on that host.
	  -n no execute mode
	  -v verbose mode
	endKat
}

# ---------------------------------------------------------------------------------------------
argv0=$0

shell=ksh
verbose=0
purge_all=0
ssh_host=""						# if -h given, this is set to run the ovs commands via ssh on remote host, else we'll just execute here
ssh_opts="-o ConnectTimeout=2 -o StrictHostKeyChecking=no -o PreferredAuthentications=publickey"
forreal=1
limit=""
rhost="localhost=$(hostname)"	# target host name for error messages only

while [[ $1 == -* ]]
do
	case $1 in
		-a)		purge_all=1;;
		-h)		
				if [[ $2 != $(hostname) && $2 != "localhost" ]]
				then
					ssh_host="ssh $ssh_opts $2";
					rhost="$2"
				fi
				shift
				;;

		-l)		limit+="$2 "; shift;;
		-n)	 	forreal=0;;
		-v)		verbose=1;;
		-\?) 	usage
				exit 1
				;;
	esac

	shift
done


if (( $(id -u) != 0 ))
then
	sudo="sudo"					# must use sudo for the ovs-vsctl commands
fi


(
	cat <<-endKat | timeout 15 $ssh_host ksh					# hack to bundle the remote commands since we cannot install software there
		$sudo ovs-vsctl list QoS |sed 's/^/QOS /'
		$sudo ovs-vsctl list Port | sed 's/^/PORT /'
		$sudo ovs-vsctl list Queue | sed 's/^/QUEUE /'
	endKat

	if (( $? > 0 ))
	then
		echo "ERROR!"
	fi
)| awk -v limit_lst="${limit:-all}" -v purge_all=${purge_all:-0} -v sudo="$sudo" -v verbose=$verbose '
	BEGIN {
		n = split( limit_lst, a, " " )
		for( i = 1; i <= n; i++ )
			lmit[a[i]] = 1
	}

	/ERROR!/ {
		print "ERROR!";
		next;
	}

	/QOS _uuid/ {
		qos[$NF] = 1;
		cur_qos = $NF;
		next;
	}

	/QUEUE _uuid/ {						# track all queues, not just those associated with a qos so we purge all orphaned queues at end
		qrefcount[$NF] += 0;
		next;
	}

	/^switch: / && NF > 1 { 			# collect switch data for purge-all
		if( limit["all"] || limit[$2] || limit[$4] )
			pa_cur_switch = $2;
		else
			pa_cur_switch=""
		next;
	}

	/^port: / && NF > 1 {					# collect switch/port info for purge all
		if( pa_cur_switch != "" )
			swpt2uuid[pa_cur_switch"-"$NF] = $2;
		next;
	}

	/QOS queues/	{
		for( i = 3; i <= NF; i++ )
		{
			gsub( "}", "", $(i) );
			gsub( ",", "", $(i) );
			split( $(i), a, "=" );
			qos2queue[cur_qos] = qos2queue[cur_qos] a[2] " ";
			qrefcount[a[2]]++;
			nqueues[cur_qos]++;
		}
	}

	/PORT _uuid/ {
		cur_port = $NF;
		next;
	}
	/PORT qos/ {
		port2qos[cur_port] = $NF;
		qos_ref[$NF] = 1;
		next;
	}

	END {
		if( purge_all )				# drop queues from all switches first else queue/qos deletes fail
		{
			for( s in port2qos )
				printf( "%s ovs-vsctl clear Port %s qos\n", sudo, s );
		}

		qlidx = 0;									# index into qos list
		for( x in nqueues )
		{
			if( purge_all || ! qos_ref[x] )			# unreferenced queue combination
			{
				if( verbose )
					printf( "delete qos: %s\n", x ) >"/dev/fd/2";
				printf( "%s ovs-vsctl destroy QoS %s\n", sudo, x );		# should use --if-exists, but backlevel ovs used with openstack prevents this

				n = split( qos2queue[x], a, " " )		# deref each individual queue in the group
				for( i = 1; i <= n; i++ )
					qrefcount[a[i]]--;
			}
			else
				if( verbose )
					printf( "keep qos: %s\n", x ) >"/dev/fd/2";
		}

		for( x in qrefcount )						# del any individual queues that ended up with a 0 refcount
			if( x != ""  &&  (purge_all || qrefcount[x] <= 0) )
			{
				printf( "%s ovs-vsctl  destroy Queue %s\n", sudo, x );
				if( verbose )
					printf( "delete queue: %s\n", x ) >"/dev/fd/2";
			}
			else
				if( x != "" &&  verbose )
					printf( "keep queue: %s\n", x ) >"/dev/fd/2";
	}
'  >/tmp/PID$$.cmds2		# snag cmds to execute in bulk if running through ssh

if ! grep -q "ERROR!" /tmp/PID$$.cmds2
then
	if [[ -s /tmp/PID$$.cmds2 ]]
	then
		if (( forreal ))
		then
			$ssh_host ksh </tmp/PID$$.cmds2
		else
			echo "would execute: "
			cat /tmp/PID$$.cmds2
		fi
	fi
else
	echo "$argv0: error running ssh commands on target-host: ${rhost#* } [FAIL]" >&2
fi

rm -f /tmp/PID$$.*

exit





_uuid               : c383cc20-7f05-45eb-9d75-1ce2b5adfbb8
bond_downdelay      : 0
bond_fake_iface     : false
bond_mode           :
bond_updelay        : 0
external_ids        :
fake_bridge         : false
interfaces          : f2d71458-67ce-4279-9e4c-b2f73c405544
lacp                :
mac                 :
name                : s7-eth2
other_config        :
qos                 :
statistics          :
status              :
tag                 :
trunks              :
vlan_mode           :

