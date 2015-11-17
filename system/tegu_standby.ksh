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

#
#	Mnemonic:	tegu_standby
#	Abstract:	Simple script to either turn on or turn off standby mode. We do this by creating/removing
#				a file in the TEGU_ETCD directory (/etc/tegu by default).
#				The first parameter must be on, or off to affect change. The parameter state will write
#				the current state to the tty and exit. Any other invocation will restult in a usage
#				message.  The command must be executed as the tegu user or it will error. When turning off
#				stand-by mode, an attempt will be made to restore the most recent checkpoint files from
#				the synchronisation archive. This can be disabled by adding a second parameter: norestore.
#
#	Date:		25 July 2014
#	Author:		E. Scott Daniels
#
#	Mod:		27 Aug 2014 - Added protection against chef running 'service tegu standby' if the node
#					has been put into active mode.
#				10 Mar 2015 - Made less chatty since it gets invoked every n seconds by tegu_ha and
#					thus pollutes the log.
# --------------------------------------------------------------------------------------------------


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

# --------------------------------------------------------------------------------------------------

export TEGU_ROOT=${TEGU_ROOT:-/var}
logd=${TEGU_LOGD:-/var/log/tegu}
libd=${TEGU_LIBD:-/var/lib/tegu}
etcd=${TEGU_ETCD:-/etc/tegu}
tegu_user=${TEGU_USER:-tegu}

standby_file=$etcd/standby				# prevents tegu_start and tegu_start_agent scripts from running
active_file=$etcd/active				# sole purpose is to prevent damage when chef runs if node has been made active

if [[ ! -d $etcd ]]
then
	echo "tegu seems not to be installed on this host: $etcd doesn't exist"
	exit 1
fi

case $1 in
	off)								# standby off mode -- tegu is allowed to be active on this host
			verify_id
			rm -f $standby_file
			touch $active_file
			echo "standby turned off"
			if [[ -z $2 || $2 != "norestore" ]]			# restore last chkpt sync if we can
			then
				echo "restoring checkpoints from synchronisation  [OK]"
				tegu_synch restore
			fi
			;;

	on)									# tegu not allowed to start; standby host
			verify_id
			touch $standby_file
			rm -f $active_file
			;;

	state)
			if [[ -f $standby_file ]]
			then
				echo "this host is a tegu standby host"
			else
				echo "this host is an active tegu host"
			fi
			;;

	-\?)	echo "usage: $0 {off [norestore]|on|state}" ;;

	*)		echo "usage: $0 {off [norestore]|on|state}"; exit 1 ;;
esac

exit 0
