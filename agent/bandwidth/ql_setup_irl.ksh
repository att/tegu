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

# -----------------------------------------------------------------------------------------------------------------
#	Mnemonic:	ql_setup_irl.ksh
#	Abstract:	Sets up ingress rate limiting bridge veth connector and flow-mods.
#
#	Author:		E. Scott Daniels
#	Date:		22 September 2014
#
#	Mods:		07 Oct 2014 - bug fix #227 - Only puts in flow mods when bridge is created
#				15 Oct 2014 - corrected bug introduced with #227 that was causing the br-rl f-mod
#						not to be created if it was deleted after the bridge was setup.
#				27 Oct 2014 - bug fix #241
#				10 Nov 2014 - Added connect timeout to ssh calls
#				17 Nov 2014	- Added timeouts on ssh commands to prevent "stalls" as were observed in pdk1.
#				04 Dec 2014 - Ensured that all crit/warn messages have a constant target host component.
#				28 Jan 2015 - Prevent an ssh call if -h indicates local host.
#				21 Jan 2015 - Set up on bridges other than br-int and to assume that br-rl name is <bridge>-rl
#						where bridge is the bridge that it is attached to (e.g. br-int-rl)
#						This script now requires ksh; reset #! (don't know why it was ever set to bash).
# -----------------------------------------------------------------------------------------------------------------

function logit
{
	echo "$(date "+%s %Y/%m/%d %H:%M:%S") $rhost $argv0: $@" >&2
}

function ensure_ok
{
	if (( $1 > 100 ))			# timeout
	then
		shift
		logit "abort: ssh timout: $@  target-host: $rhost  [FAIL]"
		rm -f $tmp/PID$$.*
		exit 1
	fi

	if (( $1 != 0 ))
	then
		shift
		logit "abort:  $@  target-host: $rhost  [FAIL]"
		rm -f $tmp/PID$$.*
		exit 1
	fi
}

function warn_if_bad
{
	if (( $1 != 0 ))
	then
		shift
		logit "$@	[WARN]"
	fi
}

# create the rate limit bridge
function mk_bridge
{
	$forreal timeout 15 $ssh $sudo /usr/bin/ovs-vsctl add-br $1-rl
	ensure_ok $? "unable to make bridge $1-rl"
	logit "created bridge $1-rl"
}

# remove the bridge
function rm_bridge
{
	if [[ -z $1 ]]
	then
		logit "no bridge passed to rm_bridge  [WARN]"
		return
	fi

	logit "deleting bridge $1-rl"
	$forreal timeout 15 $ssh $sudo /usr/bin/ovs-vsctl del-br $1-rl
}

# create our veth that will be used for the loop round
function mk_veth
{
	$forreal timeout 15 $ssh $sudo ip link add  $ve0 type veth peer name $ve1
	ensure_ok $? "unable to create veth link $ve0-$ve1"

	$forreal timeout 15 $ssh $sudo ip link set dev $ve0  up
	ensure_ok $? "unable to bring up veth link end point $ve0"

	$forreal timeout 15 $ssh $sudo ip link set dev $ve1 up
	ensure_ok $? "unable to bring up veth link end point $ve1"
	logit "created veth pair $ve0-$ve1   [OK]"
}

# delete the veth link
function rm_veth
{
	logit "deleting link"
	$forreal timeout 15 $ssh $sudo ip link set dev $ve0 down
	$forreal timeout 15 $ssh $sudo ip link set dev $ve1 down
	$forreal timeout 15 $ssh $sudo ip link delete  $ve0 type veth peer name $ve1
}

# detach the ports -- ignore output and return codes
function detach_veth
{
	$forreal timeout 15 $ssh $sudo ovs-vsctl del-port $ve0 >/dev/null 2>&1
	$forreal timeout 15 $ssh $sudo ovs-vsctl del-port $ve1 >/dev/null 2>&1
}

# attach the veth to the bridge and its rate limit (-rl) counterpart.
function attach_veth
{
	if [[ -z $1 ]]
	then
		logit "no bridge passed to attach_veth  [WARN]"
		return
	fi

	logit "cleaning previous attachments: $ve0-$ve1 to $1 and $1-rl   [OK]"
	# drop the ports if one or the other were already there (ignoring failures)
	detach_veth $1

	$forreal timeout 15 $ssh $sudo ovs-vsctl add-port $1 $ve0 			#-- set interface $ve0  ofport=4000
	ensure_ok $? "unable to attach veth $ve0 to $1"

	$forreal timeout 15 $ssh $sudo ovs-vsctl add-port $1-rl  $ve1 		#-- set interface $ve1  ofport=4001
	ensure_ok $? "unable to attach veth $ve1 to $1-rl"

	logit "attached $ve0-$ve1 to $1 and $1-rl   [OK]"
}

function usage
{
	echo "$argv0 version 2.0/11216" >&2
	echo "usage: $argv0 [-D] [-h host] [-n] [-p link-prefix] [bridge(s)]" >&2
	echo "   -D causes all rate limiting bridges, ports and links to be removed"
	echo "	bridge is the bridge that the rate limit bridge should be attached to"
	echo "  if not supplied, all bridges listed in the agent config file are setup/deleted."
}

# -------------------------------------------------------------------------------------------
if (( $( id -u ) != 0 ))
then
	sudo="sudo"
fi
config="${TEGU_AGENT_CONFIG:-tegu_agent.cfg}"

tmp=${TMP:-/tmp}
argv0="${0##*/}"
ssh_opts="-o ConnectTimeout=2 -o StrictHostKeyChecking=no -o PreferredAuthentications=publickey"

ssh=""									# if -h given, these get populated with needed remote host information
rhost="$(hostname)"						# remote host name for messages
rhost_parm=""							# remote host parameter (-h xxx) for commands that need it

forreal=""
no_exe=""
traceon="set -x"
traceoff="set +x"
link_prefix="qosirl"
force_attach=0
delete=0

typeset -C bandwidth											# must ensure this is set to handle missing config file
ql_parse_config -f $config >/tmp/PID$$.cfg && . /tmp/PID$$.cfg		# xlate config file into variables and source them

while [[ $1 == "-"* ]]
do
	case $1 in
		-D)	delete=1;;
		-h)	
			if [[ $2 != $rhost  &&  $2 != "localhost" ]]
			then
				rhost="$2"
				rhost_parm="-h $2"
				ssh="ssh -n $ssh_opts $2" 		# CAUTION: this MUST have -n since we don't redirect stdin to ssh
			fi
			shift
			;;

		-n)	
			no_exec="-n"
			forreal="echo noexec (-n) is set, would run: "
			traceon=""
			traceoff=""
			;;

		-p)	link_prefix="$2"; shift;;

		-\?)	usage;
				exit;;
	esac

	shift
done

if [[ -n $1 ]]
then
	bridge_list="$@"							# assume bridges to operate supplied on the command line
else
	bridge_list="${bandwidth.rate_limit}"		# else operate on all rate limit bridges in the config
fi

for bridge in $bridge_list
do
	ve0="${bridge}-${link_prefix}0"		# bloody namespace is flat, add bridge as a prefix
	ve1="${bridge}-${link_prefix}1"		# veth pair endpint names that go into OVS

	if (( delete ))
	then
		logit "deleting ingress rate limiting configuration (bridge, ports, veth pair) for $bridge   [OK]"
		detach_veth	$bridge					# bring the ports down and remove from the bridges
		rm_veth	$bridge						# delete the link
		rm_bridge $bridge					# finally drop the bridge
	else

		if [[ -e /etc/tegu/no_irl ]]
		then
			logit "abort: /etc/tegu/no_irl file exists which prevents setting up ingress rate limiting bridge and flow-mods	[WARN]"
			exit 1
		fi

		bridge_list=/tmp/PID$$.brdge
		link_list=/tmp/PID$$.link

		timeout 15 $ssh $sudo ovs-vsctl -d bare list bridge|grep name >$bridge_list
		ensure_ok $? "unable to get bridge list"

		add_fmod=0
		if ! grep -q $bridge-rl $bridge_list
		then
			logit "making bridge: $bridge-rl	[OK]"
			mk_bridge $bridge
			force_attach=1
			add_fmod=1				# no flow-mod if new bridge; cause creation
		else
			timeout 15 $ssh $sudo ovs-ofctl dump-flows $bridge-rl |grep -q cookie=0xdead
			if (( $? > 0 ))
			then
				add_fmod=1			# f-mod gone missing; force creation
			fi
		fi

		if (( add_fmod ))
		then
			# bug fix #227 -- only replace the flow mod when bridge is created, or if we cannot find it
			send_ovs_fmod $rhost_parm $no_exec -t 0 --match --action -b add 0xdead $bridge-rl 		# default f-mod for *-rl that bounces packets out from whence they came
			ensure_ok $? "unable to set flow-mod on $bridge-rl"
		fi

		timeout 15 $ssh ip link > $link_list
		ensure_ok $? "unable to generate a list of links"

		if ! grep -q "$link_prefix" $link_list			# no veth found, make it
		then
			mk_veth	$bridge
			attach_veth $bridge
		else
			c=0
			if (( ! force_attach ))		# don't need to spend time if force was set
			then
				ovs_sp2uuid $rhost_parm -a >/tmp/PID$$.udata				# fix #241; ensure that veth are attached to bridges
				ensure_ok $? "unable to get ovs uuid data from $rhost"
				c=$( grep -c $link_prefix /tmp/PID$$.udata )
			fi
			if (( c != 2 ))				# didn't exist, or pair existed, but if we had to create *-rl then we must attach it
			then
				attach_veth	$bridge
			fi
		fi


		$forreal timeout 15 $ssh $sudo ovs-ofctl mod-port $bridge $ve0 noflood		# we should always do this
		warn_if_bad $? "unable to set no flood on $bridge:$ve0"
	fi
done

rm -f $tmp/PID$$.*
exit 0


