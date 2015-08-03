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

#!/usr/bin/env ksh
#
#	Mnemonic:	start_agent
#	Abstract:	Simple diddy to start the tegu_agent pointing at the proper spots for log and such.
#				By default the lib and log directories are assumed to be in /var/lib/tegu and /var/log/tegu
#				however these can be overridden with TEGU_LOGD and TEGU_LIBD environment variables if
#				necessary.
#	Date:		05 May 2014
#	Author:		E. Scott Daniels
#
#	Mod:		24 Jul 2014 - Support for standby host
# --------------------------------------------------------------------------------------------------

export TEGU_ROOT=${TEGU_ROOT:-/var}
logd=${TEGU_LOGD:-/var/log/tegu}
libd=${TEGU_LIBD:-/var/lib/tegu}
etcd=${TEGU_ETCD:-/etc/tegu}
tegu_user=${TEGU_USER:-tegu}

standby_file=$etcd/standby

if [[ -f $standby_file ]]
then
	echo "not starting agents -- this is a standby host  [WARN]"
	echo "execute 'tegu_standby off' to turn stand-by mode off and then attempt to start with $0"
	exit 0
fi

if ! cd $logd
then
	if ! mkdir $logd
	then
		echo "unable to find or mk $logd  [FAIL]"
		exit 1
	fi
fi

whoiam=$( id -n -u )
if [[ $whoiam != $tegu_user ]]
then
	echo "tegu_agent must be started under the user name tegu  ($(whoami) is not acceptable)     [FAIL]"
	echo '`sudo su tegu` and rerun this script'
	echo ""
	exit 1
fi

# all of these must be in the path or the agent cannot drive them, so verify now before starting agent(s)
error=0
for p in map_mac2phost  setup_ovs_intermed  create_ovs_queues  ovs_sp2uuid send_ovs_fmod  tegu_req purge_ovs_queues
do
	if ! which $p >/dev/null 2>&1
	then
		error=1
		echo "CRI: unable to find programme/script in path:  $p   [FAIL]"
	fi
done

if (( error ))
then
	exit 1
fi


# start n agents or the agents listed on the command line if not null
if [[ -z $1 ]]
then
	set 1 2 3 4 5
fi

while [[ -n $1 ]]
do
	ps -elf|grep -q "tegu_agent [-]i $1"
	if (( $? > 0 ))
	then
		echo "staring tegu_agent $1   [OK]"
		nohup tegu_agent -i $1 -l $logd >tegu_agent$1.std 2>&1 &
	else
		echo "tegu_agent $1 is already running, not started   [OK]"
	fi

	shift
done
exit 0
