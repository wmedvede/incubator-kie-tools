/*
 * Copyright 2018 Red Hat, Inc. and/or its affiliates.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package org.drools.workbench.screens.scenariosimulation.client.widgets;

import com.ait.lienzo.client.core.event.NodeMouseDoubleClickHandler;
import com.ait.lienzo.test.LienzoMockitoTestRunner;
import org.drools.workbench.screens.scenariosimulation.client.models.ScenarioGridModel;
import org.drools.workbench.screens.scenariosimulation.client.renderers.ScenarioGridRenderer;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.mockito.Mock;
import org.uberfire.ext.wires.core.grids.client.widget.layer.GridSelectionManager;
import org.uberfire.ext.wires.core.grids.client.widget.layer.pinning.GridPinnedModeManager;

import static org.drools.workbench.screens.scenariosimulation.client.TestUtils.getSimulation;
import static org.junit.Assert.assertNotNull;
import static org.mockito.Matchers.anyInt;
import static org.mockito.Matchers.anyObject;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;

@RunWith(LienzoMockitoTestRunner.class)
public class ScenarioGridTest {

    @Mock
    private ScenarioGridModel mockScenarioGridModel;
    @Mock
    private ScenarioGridLayer mockScenarioGridLayer;
    @Mock
    private ScenarioGridRenderer mockScenarioGridRenderer;
    @Mock
    private ScenarioGridPanel mockScenarioGridPanel;

    private ScenarioGrid scenarioGrid;

    @Before
    public void setup() {
        scenarioGrid = new ScenarioGrid(mockScenarioGridModel, mockScenarioGridLayer, mockScenarioGridRenderer, mockScenarioGridPanel);
    }

    @Test
    public void getGridMouseDoubleClickHandler() {
        NodeMouseDoubleClickHandler retrieved = scenarioGrid.getGridMouseDoubleClickHandler(mock(GridSelectionManager.class), mock(GridPinnedModeManager.class));
        assertNotNull(retrieved);
    }

    @Test
    public void setContent() {
        int cols = 2;
        int rows = 2;
        scenarioGrid.setContent(getSimulation(cols, rows));
        // cols + 1 because there is also the description column
        verify(mockScenarioGridModel, times(cols + 1)).insertColumn(anyInt(), anyObject());
        verify(mockScenarioGridModel, times(rows)).insertRow(anyInt(), anyObject());
    }
}