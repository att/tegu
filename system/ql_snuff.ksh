#!/usr/bin/env ksh
# vi: ts=4:

#
#	Mnemonic:	ql_snuff
#	Abstract:	Snuff out all of the underlying things (fmods, queues, iptables rules) that
#				exist to support QoS-Lite. This should be exectued only as a last ditch effort
#				when you think that qlite is causing so many problms that you want to pull the
#				plug and let it all go down the drain.
#
#	Date:		12 Feb 2014
#	Author:		E. Scott Daniels
#
#	Mod:
# --------------------------------------------------------------------------------------------------

trap "rm -f /tmp/PID$$.*" 1 2 3 15 EXIT

function verify_id
{
	whoiam=$( id -n -u )
	if [[ $whoiam != $tegu_user ]]
	then
		echo "Only the tegu user ($tegu_user) can affect the state; ($(whoami) is not acceptable)     [FAIL]"
		echo "'sudo su $tegu_user' and rerun this script"
		echo ""
		exit 1
	fi
}

function tty_rewrite
{
	echo "$(date) $1" >>$log
	if (( ! agent_driven ))
	then
		printf "\r\033[0K%s" "$1"
	fi
}



# Delete the iptables rules in mangle that do the right thing for our DSCP marked traffic 
# This must also handle all of the bloody routers that are created in namespaces, so we first generate a 
# set of commands for the main iptables, then generate the same set for each nameespace. This all goes
# into a single command file which is then fed into ssh to be executed on the target host. 
#
# we assume that this funciton is run asynch and so we capture all output into a file that can be spit out
# at the end.
function purge_iptables
{
	typeset cmd_string=""					# normall space iptables command list
	typeset cmd_file=/tmp/PID$$.cmds		# cmds to send to the remote to set ip stuff
	typeset nslist="/tmp/PID$$.nslist"		# list of name spaces from the remote host
	typeset err_file="/tmp/PID$$.ipterr"
	typeset diffserv="184 104 72"			# these MUST be 4x the DSCP values

	thost="$1"

	timeout 300 $ssh_cmd ip netns list >$nslist 2>$err_file
	if (( $? != 0 ))
	then
		echo "unable to get network name space list from target-host: ${thost#* }  [FAIL]" >&2
		sed 's/^/purge_iptables:/' $err_file >&2 
		return 1
	fi

	typeset iptables_del_base="sudo iptables -f -D POSTROUTING -t mangle -m dscp --dscp"	# various pieces of the command string
	typeset iptables_tail="-j CLASSIFY --set-class"

	typeset iptables_nsbase="sudo ip netns exec" 										# must insert name space name between base and mid
	typeset iptables_del_mid="iptables -f -D POSTROUTING -t mangle -m dscp --dscp"			# reset for the name space specific command
	
	(																# create the commands to send; first the master iptables rules, then rules for each name space
		echo "$iptables_del_base 0 $iptables_tail 1:2;" 
		for d in ${diffserv//,/ }													# d will be 4x the value that iptables needs
		do
			echo "$iptables_del_base $((d/4)) $iptables_tail 1:6;"					# add in delete commands
		done 

		while read ns 																# for each name space we found
		do
			echo "$iptables_nsbase $ns $iptables_del_mid 0 $iptables_tail 1:2;" 				# odd ball delete case first
			for d in ${diffserv//,/ }
			do
				echo "$iptables_nsbase $ns $iptables_del_mid $((d/4)) $iptables_tail 1:6;"			# add in delete commands
			done 
		done <$nslist 
	) >$cmd_file

	if [[ -z $thost  || $thost == "localhost" ]]	# local host -- just pump into ksh
	then
		ssh_host="ksh"
	else
		typeset ssh_cmd="ssh -T $ssh_opts $thost" 	# different than what we usually use NO -n supplied!!
	fi

	rc=0											# overall return code 
	if [[ -z $really ]]								# empty string means we're live
	then
		$forreal timeout 100 $ssh_cmd <$cmd_file >$err_file 2>&1
		rc=$?
		if (( rc != 0 ))
		then
			if ! grep -q "No chain/target/match by that name" $err_file						# not an error; it wasn't there to begin with
			then
				echo "unable to purge iptables on target-host: ${thost#* }  [FAIL]" >&2
				sed 's/^/purge_iptables:/' $err_file >&2
			else
				rc=0
				echo "iptables deleted for mangle rules on target-host: ${thosts#* }" >&2
			fi
		else
			echo "iptables deleted for mangle rules on target-host: ${thosts#* }" >&2
		fi
	else
		sed "s/^/iptables purge: $no_exec_str /" $cmd_file >&2
	fi

	rm -f /tmp/PID$$.*
	return $rc
}


# --------------------------------------------------------------------------------------------------

export TEGU_ROOT=${TEGU_ROOT:-/var}
logd=${TEGU_LOGD:-/var/log/tegu}
libd=${TEGU_LIBD:-/var/lib/tegu}
etcd=${TEGU_ETCD:-/etc/tegu}
chkptd=$TEGU_ROOT/chkpt
tegu_user=${TEGU_USER:-tegu}

ssh_opts="-o StrictHostKeyChecking=no -o PreferredAuthentications=publickey"
log=/tmp/qlite_snuff.log

really="-n"
forreal="echo would run:"
do_iptables=1
do_fmods=1
do_queues=1

agent_driven=0

while [[ $1 == -* ]]
do
	case $1 in 
		-a)		agent_driven=1;;
		-F)		do_fmods=0;;
		-Q)		do_queues=0;;
		-I)		do_iptables=0;;

		-b)		only_bridges="$2"; shift;;
		-f)		really=""; forreal="";;
		-p)		>$log;;
		-n)		really="-n"
				forreal="echo would run:"
				ssh_cmd="ssh $ssh_opts $h "
				;;

		-*)		echo "unrecognised option: $1"
				echo "usage: $0 [-p] [-b bridges] [-f] [-n] host1 [host2....hostn]"
				exit 1
	esac

	shift
done

host_list="$@"
# remove flow-mods
echo "removing flow-mods"

fcount=0
fecount=0
qcount=0
qecount=0
icount=0
iecount=0
hcount=0

for h in ${host_list:-localhost}
do
	(( hcount++  ))
	if [[ $h == "localhost" ]]
	then
		target=""						# no target for queues
	else
		target="-h $h"					# running remotely we must pass it along this way
	fi

	if (( do_fmods ))
	then
		if [[ -z $only_bridges ]]
		then
			blist=$( $ssh_cmd sudo ovs-vsctl show | grep Bridge | awk ' { gsub( "\"", "", $0 ); l = l $2 " " } END { print l } ' )
		else
			blist="$only_bridges"
		fi

		for b in $blist
		do
			for cookie in 0xbeef 0xdead 0xe5d 0xdeaf 0xfeed 0xface
			do
				tty_rewrite "$h remove fmods: $b $cookie"
				send_ovs_fmod $really -h ${h:-nohost} -t 2 --match --action del $cookie $b >>$log 2>&1
				if (( $? != 0 ))
				then
					(( fecount++ ))
					printf "$(date) ... failed\n"
					printf "FAILED\n\n" >>$log
				fi

				(( fcount++ ))
			done
		done
	fi

	if (( do_queues ))
	then
		tty_rewrite "$h  purging queues"
		purge_ovs_queues $really -a $target >>$log 2>&1
		if (( $? != 0 ))
		then
			(( qecount++ ))
			printf "$(date) ... failed\n"
			printf "FAILED\n\n" >>$log
		fi
		(( qcount++ ))
	fi
	
	if (( do_iptables ))
	then
		tty_rewrite "$h removing iptables rules"
		purge_iptables $h >>$log 2>&1
		if (( $? != 0 ))
		then
			(( iecount++ ))
			printf "$(date) ... failed\n"
			printf "FAILED\n\n" >>$log
		fi
		(( icount++ ))
	fi
done

tty_rewrite ""
printf "hosts:           %4d\n" "$hosts"
printf "fmod purges:     %4d\n" "$fcount"
printf "fmod errors:     %4d\n" "$fecount"
printf "queue purges:    %4d\n" "$qcount"
printf "queue errors:    %4d\n" "$qecount"
printf "iptables purges: %4d\n" "$icount"
printf "iptables errors: %4d\n" "$iecount"

if (( agent_driven ))
then
	cat $log >&2
fi

exit 0

