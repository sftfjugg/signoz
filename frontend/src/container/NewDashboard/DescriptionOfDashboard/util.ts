import { DashboardData } from 'types/api/dashboard/getAll';

export function downloadObjectAsJson(
	exportObj: unknown,
	exportName: string,
): void {
	const dataStr = `data:text/json;charset=utf-8,${encodeURIComponent(
		JSON.stringify(exportObj),
	)}`;
	const downloadAnchorNode = document.createElement('a');
	downloadAnchorNode.setAttribute('href', dataStr);
	downloadAnchorNode.setAttribute('download', `${exportName}.json`);
	document.body.appendChild(downloadAnchorNode); // required for firefox
	downloadAnchorNode.click();
	downloadAnchorNode.remove();
}

export function cleardQueryData(param: DashboardData): DashboardData {
	return {
		...param,
		widgets: param.widgets?.map((widget) => ({
			...widget,
			queryData: {
				...widget.queryData,
				data: {
					queryData: [],
				},
			},
		})),
	};
}
