// Copyright (C) 2020 Storj Labs, Inc.
// See LICENSE for copying information.

import { StoreModule } from '@/store';
import {
    AccessGrant,
    AccessGrantCursor,
    AccessGrantsApi,
    AccessGrantsOrderBy,
    AccessGrantsPage,
    DurationPermission,
    GatewayCredentials,
} from '@/types/accessGrants';
import { SortDirection } from '@/types/common';

export const ACCESS_GRANTS_ACTIONS = {
    FETCH: 'fetchAccessGrants',
    CREATE: 'createAccessGrant',
    DELETE: 'deleteAccessGrants',
    CLEAR: 'clearAccessGrants',
    GET_GATEWAY_CREDENTIALS: 'getGatewayCredentials',
    SET_ACCESS_GRANTS_WEB_WORKER: 'setAccessGrantsWebWorker',
    STOP_ACCESS_GRANTS_WEB_WORKER: 'stopAccessGrantsWebWorker',
    SET_SEARCH_QUERY: 'setAccessGrantsSearchQuery',
    SET_SORT_BY: 'setAccessGrantsSortingBy',
    SET_SORT_DIRECTION: 'setAccessGrantsSortingDirection',
    SET_DURATION_PERMISSION: 'setAccessGrantsDurationPermission',
    TOGGLE_SELECTION: 'toggleAccessGrantsSelection',
    TOGGLE_BUCKET_SELECTION: 'toggleBucketSelection',
    CLEAR_SELECTION: 'clearAccessGrantsSelection',
};

export const ACCESS_GRANTS_MUTATIONS = {
    SET_PAGE: 'setAccessGrants',
    SET_GATEWAY_CREDENTIALS: 'setGatewayCredentials',
    SET_ACCESS_GRANTS_WEB_WORKER: 'setAccessGrantsWebWorker',
    STOP_ACCESS_GRANTS_WEB_WORKER: 'stopAccessGrantsWebWorker',
    TOGGLE_SELECTION: 'toggleAccessGrantsSelection',
    TOGGLE_BUCKET_SELECTION: 'toggleBucketSelection',
    CLEAR_SELECTION: 'clearAccessGrantsSelection',
    CLEAR: 'clearAccessGrants',
    CHANGE_SORT_ORDER: 'changeAccessGrantsSortOrder',
    CHANGE_SORT_ORDER_DIRECTION: 'changeAccessGrantsSortOrderDirection',
    SET_SEARCH_QUERY: 'setAccessGrantsSearchQuery',
    SET_PAGE_NUMBER: 'setAccessGrantsPage',
    SET_DURATION_PERMISSION: 'setAccessGrantsDurationPermission',
};

const {
    SET_PAGE,
    TOGGLE_SELECTION,
    TOGGLE_BUCKET_SELECTION,
    CLEAR_SELECTION,
    CLEAR,
    CHANGE_SORT_ORDER,
    CHANGE_SORT_ORDER_DIRECTION,
    SET_SEARCH_QUERY,
    SET_PAGE_NUMBER,
    SET_DURATION_PERMISSION,
    SET_GATEWAY_CREDENTIALS,
    SET_ACCESS_GRANTS_WEB_WORKER,
    STOP_ACCESS_GRANTS_WEB_WORKER,
} = ACCESS_GRANTS_MUTATIONS;

export class AccessGrantsState {
    public cursor: AccessGrantCursor = new AccessGrantCursor();
    public page: AccessGrantsPage = new AccessGrantsPage();
    public selectedAccessGrantsIds: string[] = [];
    public selectedBucketNames: string[] = [];
    public permissionNotBefore: Date = new Date();
    public permissionNotAfter: Date = new Date('2200-01-01');
    public gatewayCredentials: GatewayCredentials = new GatewayCredentials();
    public accessGrantsWebWorker: Worker | null = null;
    public isAccessGrantsWebWorkerReady: boolean = false;
}

/**
 * creates access grants module with all dependencies
 *
 * @param api - accessGrants api
 */
export function makeAccessGrantsModule(api: AccessGrantsApi): StoreModule<AccessGrantsState> {
    return {
        state: new AccessGrantsState(),
        mutations: {
            [SET_ACCESS_GRANTS_WEB_WORKER](state: AccessGrantsState): void {
                state.accessGrantsWebWorker = new Worker('@/../static/wasm/accessGrant.worker.js', { type: 'module' });
                state.accessGrantsWebWorker.onmessage = (event: MessageEvent) => {
                    const data = event.data;
                    if (data !== 'configured') {
                        console.error('Failed to configure access grants web worker');

                        return;
                    }

                    state.isAccessGrantsWebWorkerReady = true;
                };
                state.accessGrantsWebWorker.onerror = (error: ErrorEvent) => {
                    console.error(`Failed to configure access grants web worker. ${error.message}`);
                };
            },
            [STOP_ACCESS_GRANTS_WEB_WORKER](state: AccessGrantsState): void {
                state.accessGrantsWebWorker?.postMessage({
                    'type': 'Stop',
                });
                state.accessGrantsWebWorker = null;
                state.isAccessGrantsWebWorkerReady = false;
            },
            [SET_PAGE](state: AccessGrantsState, page: AccessGrantsPage) {
                state.page = page;
                state.page.accessGrants = state.page.accessGrants.map(accessGrant => {
                    if (state.selectedAccessGrantsIds.includes(accessGrant.id)) {
                        accessGrant.isSelected = true;
                    }

                    return accessGrant;
                });
            },
            [SET_GATEWAY_CREDENTIALS](state: AccessGrantsState, credentials: GatewayCredentials) {
                state.gatewayCredentials = credentials;
            },
            [SET_PAGE_NUMBER](state: AccessGrantsState, pageNumber: number) {
                state.cursor.page = pageNumber;
            },
            [SET_SEARCH_QUERY](state: AccessGrantsState, search: string) {
                state.cursor.search = search;
            },
            [SET_DURATION_PERMISSION](state: AccessGrantsState, permission: DurationPermission) {
                state.permissionNotBefore = permission.notBefore;
                state.permissionNotAfter = permission.notAfter;
            },
            [CHANGE_SORT_ORDER](state: AccessGrantsState, order: AccessGrantsOrderBy) {
                state.cursor.order = order;
            },
            [CHANGE_SORT_ORDER_DIRECTION](state: AccessGrantsState, direction: SortDirection) {
                state.cursor.orderDirection = direction;
            },
            [TOGGLE_SELECTION](state: AccessGrantsState, accessGrant: AccessGrant) {
                if (!state.selectedAccessGrantsIds.includes(accessGrant.id)) {
                    state.page.accessGrants.forEach((grant: AccessGrant) => {
                        if (grant.id === accessGrant.id) {
                            grant.isSelected = true;
                        }
                    });
                    state.selectedAccessGrantsIds.push(accessGrant.id);

                    return;
                }

                state.page.accessGrants.forEach((grant: AccessGrant) => {
                    if (grant.id === accessGrant.id) {
                        grant.isSelected = false;
                    }
                });
                state.selectedAccessGrantsIds = state.selectedAccessGrantsIds.filter(accessGrantId => {
                    return accessGrant.id !== accessGrantId;
                });
            },
            [TOGGLE_BUCKET_SELECTION](state: AccessGrantsState, bucketName: string) {
                if (!state.selectedBucketNames.includes(bucketName)) {
                    state.selectedBucketNames.push(bucketName);

                    return;
                }

                state.selectedBucketNames = state.selectedBucketNames.filter(name => {
                    return bucketName !== name;
                });
            },
            [CLEAR_SELECTION](state: AccessGrantsState) {
                state.selectedBucketNames = [];
                state.selectedAccessGrantsIds = [];
                state.page.accessGrants = state.page.accessGrants.map((accessGrant: AccessGrant) => {
                    accessGrant.isSelected = false;

                    return accessGrant;
                });
            },
            [CLEAR](state: AccessGrantsState) {
                state.cursor = new AccessGrantCursor();
                state.page = new AccessGrantsPage();
                state.selectedAccessGrantsIds = [];
                state.selectedBucketNames = [];
                state.permissionNotBefore = new Date();
                state.permissionNotAfter = new Date('2200-01-01');
                state.gatewayCredentials = new GatewayCredentials();
            },
        },
        actions: {
            setAccessGrantsWebWorker: function({commit}: any): void {
                commit(SET_ACCESS_GRANTS_WEB_WORKER);
            },
            stopAccessGrantsWebWorker: function({commit}: any): void {
                commit(STOP_ACCESS_GRANTS_WEB_WORKER);
            },
            fetchAccessGrants: async function ({commit, rootGetters, state}, pageNumber: number): Promise<AccessGrantsPage> {
                const projectId = rootGetters.selectedProject.id;
                commit(SET_PAGE_NUMBER, pageNumber);

                const accessGrantsPage: AccessGrantsPage = await api.get(projectId, state.cursor);
                commit(SET_PAGE, accessGrantsPage);

                return accessGrantsPage;
            },
            createAccessGrant: async function ({commit, rootGetters}: any, name: string): Promise<AccessGrant> {
                const accessGrant = await api.create(rootGetters.selectedProject.id, name);

                return accessGrant;
            },
            deleteAccessGrants: async function({state}: any): Promise<void> {
                await api.delete(state.selectedAccessGrantsIds);
            },
            getGatewayCredentials: async function({state, commit}: any, accessGrant: string): Promise<void> {
                const credentials: GatewayCredentials = await api.getGatewayCredentials(accessGrant);

                commit(SET_GATEWAY_CREDENTIALS, credentials);
            },
            setAccessGrantsSearchQuery: function ({commit}, search: string) {
                commit(SET_SEARCH_QUERY, search);
            },
            setAccessGrantsSortingBy: function ({commit}, order: AccessGrantsOrderBy) {
                commit(CHANGE_SORT_ORDER, order);
            },
            setAccessGrantsSortingDirection: function ({commit}, direction: SortDirection) {
                commit(CHANGE_SORT_ORDER_DIRECTION, direction);
            },
            setAccessGrantsDurationPermission: function ({commit}, permission: DurationPermission) {
                commit(SET_DURATION_PERMISSION, permission);
            },
            toggleAccessGrantsSelection: function ({commit}, accessGrant: AccessGrant): void {
                commit(TOGGLE_SELECTION, accessGrant);
            },
            toggleBucketSelection: function ({commit}, bucketName: string): void {
                commit(TOGGLE_BUCKET_SELECTION, bucketName);
            },
            clearAccessGrantsSelection: function ({commit}): void {
                commit(CLEAR_SELECTION);
            },
            clearAccessGrants: function ({commit}): void {
                commit(CLEAR);
                commit(CLEAR_SELECTION);
            },
        },
        getters: {
            selectedAccessGrants: (state: AccessGrantsState) => state.page.accessGrants.filter((grant: AccessGrant) => grant.isSelected),
        },
    };
}
