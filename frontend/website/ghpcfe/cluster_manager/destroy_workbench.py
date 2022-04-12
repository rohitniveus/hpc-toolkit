# Copyright 2022 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import argparse

from .utils import run_terraform, load_config


def destroy_workbench(workbench, token):
    '''
    Destroy a workbench.

        Parameters:
            args - an object with the following members:
                'workbench_id' - id # of the compute workbench
                'accessKey' - DB access key
    '''

    config = load_config()

    workbench.status = 't'
    workbench.cloud_state = 'dm'
    workbench.save()
    workbench_dir = config["baseDir"] / 'workbenches' / f'workbench_{workbench.id}'

    print("running destroy workbench " + str(workbench.id))
    run_terraform(workbench_dir / 'terraform' / 'google', 'destroy')
    workbench.status = 'd'
    workbench.cloud_state = 'xm'
    workbench.save()

