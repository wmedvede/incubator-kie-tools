/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *  http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

const { varsWithName, composeEnv, getOrDefault } = require("@kie-tools-scripts/build-env");

const rootEnv = require("@kie-tools/root-env/env");
const sonataflowImageCommonEnv = require("@kie-tools/sonataflow-image-common/env");
const redHatEnv = require("@osl/redhat-env/env");

module.exports = composeEnv([rootEnv, sonataflowImageCommonEnv, redHatEnv], {
  vars: varsWithName({
    OSL_MANAGEMENT_CONSOLE_IMAGE__artifactUrl: {
      default: "",
      description: "The internal repository where to find the Management Console app zip file.",
    },
    OSL_MANAGEMENT_CONSOLE_IMAGE__registry: {
      default: "registry.access.redhat.com",
      description: "The image registry.",
    },
    OSL_MANAGEMENT_CONSOLE_IMAGE__account: {
      default: "openshift-serverless-1",
      description: "The image registry account.",
    },
    OSL_MANAGEMENT_CONSOLE_IMAGE__name: {
      default: "logic-management-console-rhel8",
      description: "The image name.",
    },
    OSL_MANAGEMENT_CONSOLE_IMAGE__buildTag: {
      default: rootEnv.env.root.streamName,
      description: "The image tag.",
    },
    OSL_MANAGEMENT_CONSOLE_IMAGE__port: {
      default: 8080,
      description: "The internal container port.",
    },
  }),
  get env() {
    return {
      oslManagementConsoleImage: {
        registry: getOrDefault(this.vars.OSL_MANAGEMENT_CONSOLE_IMAGE__registry),
        account: getOrDefault(this.vars.OSL_MANAGEMENT_CONSOLE_IMAGE__account),
        name: getOrDefault(this.vars.OSL_MANAGEMENT_CONSOLE_IMAGE__name),
        buildTag: getOrDefault(this.vars.OSL_MANAGEMENT_CONSOLE_IMAGE__buildTag),
        artifactUrl: getOrDefault(this.vars.OSL_MANAGEMENT_CONSOLE_IMAGE__artifactUrl),
        version: require("../package.json").version,
        port: getOrDefault(this.vars.OSL_MANAGEMENT_CONSOLE_IMAGE__port),
      },
    };
  },
});
