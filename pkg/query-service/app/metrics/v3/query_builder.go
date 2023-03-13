package v3

import (
	"fmt"
	"strings"

	"go.signoz.io/signoz/pkg/query-service/constants"
	v3 "go.signoz.io/signoz/pkg/query-service/model/v3"
	"go.signoz.io/signoz/pkg/query-service/utils"
)

var aggregateOperatorToPercentile = map[v3.AggregateOperator]float64{
	v3.AggregateOperatorP05:         0.5,
	v3.AggregateOperatorP10:         0.10,
	v3.AggregateOperatorP20:         0.20,
	v3.AggregateOperatorP25:         0.25,
	v3.AggregateOperatorP50:         0.50,
	v3.AggregateOperatorP75:         0.75,
	v3.AggregateOperatorP90:         0.90,
	v3.AggregateOperatorP95:         0.95,
	v3.AggregateOperatorP99:         0.99,
	v3.AggregateOperatorHistQuant50: 0.50,
	v3.AggregateOperatorHistQuant75: 0.75,
	v3.AggregateOperatorHistQuant90: 0.90,
	v3.AggregateOperatorHistQuant95: 0.95,
	v3.AggregateOperatorHistQuant99: 0.99,
}

var aggregateOperatorToSQLFunc = map[v3.AggregateOperator]string{
	v3.AggregateOperatorAvg:     "avg",
	v3.AggregateOperatorMax:     "max",
	v3.AggregateOperatorMin:     "min",
	v3.AggregateOperatorSum:     "sum",
	v3.AggregateOperatorRateSum: "sum",
	v3.AggregateOperatorRateAvg: "avg",
	v3.AggregateOperatorRateMax: "max",
	v3.AggregateOperatorRateMin: "min",
}

// buildMetricsTimeSeriesFilterQuery builds the sub-query to be used for filtering
// timeseries based on search criteria
func buildMetricsTimeSeriesFilterQuery(fs *v3.FilterSet, groupTags []string, metricName string, aggregateOperator v3.AggregateOperator) (string, error) {
	var conditions []string
	conditions = append(conditions, fmt.Sprintf("metric_name = %s", utils.ClickHouseFormattedValue(metricName)))

	if fs != nil && len(fs.Items) != 0 {
		for _, item := range fs.Items {
			toFormat := item.Value
			op := strings.ToLower(strings.TrimSpace(item.Operator))
			// if the received value is an array for like/match op, just take the first value
			if op == "like" || op == "match" || op == "nlike" || op == "nmatch" {
				x, ok := item.Value.([]interface{})
				if ok {
					if len(x) == 0 {
						continue
					}
					toFormat = x[0]
				}
			}
			fmtVal := utils.ClickHouseFormattedValue(toFormat)
			switch op {
			case "eq":
				conditions = append(conditions, fmt.Sprintf("JSONExtractString(labels, '%s') = %s", item.Key, fmtVal))
			case "neq":
				conditions = append(conditions, fmt.Sprintf("JSONExtractString(labels, '%s') != %s", item.Key, fmtVal))
			case "in":
				conditions = append(conditions, fmt.Sprintf("JSONExtractString(labels, '%s') IN %s", item.Key, fmtVal))
			case "nin":
				conditions = append(conditions, fmt.Sprintf("JSONExtractString(labels, '%s') NOT IN %s", item.Key, fmtVal))
			case "like":
				conditions = append(conditions, fmt.Sprintf("like(JSONExtractString(labels, '%s'), %s)", item.Key, fmtVal))
			case "nlike":
				conditions = append(conditions, fmt.Sprintf("notLike(JSONExtractString(labels, '%s'), %s)", item.Key, fmtVal))
			case "match":
				conditions = append(conditions, fmt.Sprintf("match(JSONExtractString(labels, '%s'), %s)", item.Key, fmtVal))
			case "nmatch":
				conditions = append(conditions, fmt.Sprintf("not match(JSONExtractString(labels, '%s'), %s)", item.Key, fmtVal))
			case "gt":
				conditions = append(conditions, fmt.Sprintf("JSONExtractString(labels, '%s') > %s", item.Key, fmtVal))
			case "gte":
				conditions = append(conditions, fmt.Sprintf("JSONExtractString(labels, '%s') >= %s", item.Key, fmtVal))
			case "lt":
				conditions = append(conditions, fmt.Sprintf("JSONExtractString(labels, '%s') < %s", item.Key, fmtVal))
			case "lte":
				conditions = append(conditions, fmt.Sprintf("JSONExtractString(labels, '%s') <= %s", item.Key, fmtVal))
			case "contains":
				conditions = append(conditions, fmt.Sprintf("like(JSONExtractString(labels, '%s'), %s)", item.Key, fmtVal))
			case "ncontains":
				conditions = append(conditions, fmt.Sprintf("notLike(JSONExtractString(labels, '%s'), %s)", item.Key, fmtVal))
			case "exists":
				conditions = append(conditions, fmt.Sprintf("has(JSONExtractKeys(labels), %s)", item.Key))
			case "nexists":
				conditions = append(conditions, fmt.Sprintf("not has(JSONExtractKeys(labels), %s)", item.Key))
			default:
				return "", fmt.Errorf("unsupported operation")
			}
		}
	}
	queryString := strings.Join(conditions, " AND ")

	var selectLabels string
	if aggregateOperator == v3.AggregateOperatorNoOp || aggregateOperator == v3.AggregateOperatorRate {
		selectLabels = "labels,"
	} else {
		for _, tag := range groupTags {
			selectLabels += fmt.Sprintf(" JSONExtractString(labels, '%s') as %s,", tag, tag)
		}
	}

	filterSubQuery := fmt.Sprintf("SELECT %s fingerprint FROM %s.%s WHERE %s", selectLabels, constants.SIGNOZ_METRIC_DBNAME, constants.SIGNOZ_TIMESERIES_TABLENAME, queryString)

	return filterSubQuery, nil
}

func buildMetricQuery(start, end, step int64, mq *v3.BuilderQuery, tableName string) (string, error) {

	filterSubQuery, err := buildMetricsTimeSeriesFilterQuery(mq.Filters, mq.GroupBy, mq.AggregateAttribute, mq.AggregateOperator)
	if err != nil {
		return "", err
	}

	samplesTableTimeFilter := fmt.Sprintf("metric_name = %s AND timestamp_ms >= %d AND timestamp_ms <= %d", utils.ClickHouseFormattedValue(mq.AggregateAttribute), start, end)

	// Select the aggregate value for interval
	queryTmpl :=
		"SELECT %s" +
			" toStartOfInterval(toDateTime(intDiv(timestamp_ms, 1000)), INTERVAL %d SECOND) as ts," +
			" %s as value" +
			" FROM " + constants.SIGNOZ_METRIC_DBNAME + "." + constants.SIGNOZ_SAMPLES_TABLENAME +
			" GLOBAL INNER JOIN" +
			" (%s) as filtered_time_series" +
			" USING fingerprint" +
			" WHERE " + samplesTableTimeFilter +
			" GROUP BY %s" +
			" ORDER BY %s ts"

	// tagsWithoutLe is used to group by all tags except le
	// This is done because we want to group by le only when we are calculating quantile
	// Otherwise, we want to group by all tags except le
	tagsWithoutLe := []string{}
	for _, tag := range mq.GroupBy {
		if tag != "le" {
			tagsWithoutLe = append(tagsWithoutLe, tag)
		}
	}

	groupByWithoutLe := groupBy(tagsWithoutLe...)
	groupTagsWithoutLe := groupSelect(tagsWithoutLe...)
	orderWithoutLe := orderBy(mq.OrderBy, tagsWithoutLe)

	groupBy := groupBy(mq.GroupBy...)
	groupTags := groupSelect(mq.GroupBy...)
	orderBy := orderBy(mq.OrderBy, mq.GroupBy)

	if len(orderBy) != 0 {
		orderBy += ","
	}

	switch mq.AggregateOperator {
	case v3.AggregateOperatorRate:
		// Calculate rate of change of metric for each unique time series
		groupBy = "fingerprint, ts"
		groupTags = "fingerprint,"
		op := "max(value)" // max value should be the closest value for point in time
		subQuery := fmt.Sprintf(
			queryTmpl, "any(labels) as labels, "+groupTags, step, op, filterSubQuery, groupBy, orderBy,
		) // labels will be same so any should be fine
		query := `SELECT %s ts, runningDifference(value)/runningDifference(ts) as value FROM(%s)`

		query = fmt.Sprintf(query, "labels as fullLabels,", subQuery)
		return query, nil
	case v3.AggregateOperatorSumRate:
		rateGroupBy := "fingerprint, " + groupBy
		rateGroupTags := "fingerprint, " + groupTags
		rateOrderBy := "fingerprint, " + orderBy
		op := "max(value)"
		subQuery := fmt.Sprintf(
			queryTmpl, rateGroupTags, step, op, filterSubQuery, rateGroupBy, rateOrderBy,
		) // labels will be same so any should be fine
		query := `SELECT %s ts, runningDifference(value)/runningDifference(ts) as value FROM(%s) OFFSET 1`
		query = fmt.Sprintf(query, groupTags, subQuery)
		query = fmt.Sprintf(`SELECT %s ts, sum(value) as value FROM (%s) GROUP BY %s ORDER BY %s ts`, groupTags, query, groupBy, orderBy)
		return query, nil
	case
		v3.AggregateOperatorRateSum,
		v3.AggregateOperatorRateMax,
		v3.AggregateOperatorRateAvg,
		v3.AggregateOperatorRateMin:
		op := fmt.Sprintf("%s(value)", aggregateOperatorToSQLFunc[mq.AggregateOperator])
		subQuery := fmt.Sprintf(queryTmpl, groupTags, step, op, filterSubQuery, groupBy, orderBy)
		query := `SELECT %s ts, runningDifference(value)/runningDifference(ts) as value FROM(%s) OFFSET 1`
		query = fmt.Sprintf(query, groupTags, subQuery)
		return query, nil
	case
		v3.AggregateOperatorP05,
		v3.AggregateOperatorP10,
		v3.AggregateOperatorP20,
		v3.AggregateOperatorP25,
		v3.AggregateOperatorP50,
		v3.AggregateOperatorP75,
		v3.AggregateOperatorP90,
		v3.AggregateOperatorP95,
		v3.AggregateOperatorP99:
		op := fmt.Sprintf("quantile(%v)(value)", aggregateOperatorToPercentile[mq.AggregateOperator])
		query := fmt.Sprintf(queryTmpl, groupTags, step, op, filterSubQuery, groupBy, orderBy)
		return query, nil
	case v3.AggregateOperatorHistQuant50, v3.AggregateOperatorHistQuant75, v3.AggregateOperatorHistQuant90, v3.AggregateOperatorHistQuant95, v3.AggregateOperatorHistQuant99:
		rateGroupBy := "fingerprint, " + groupBy
		rateGroupTags := "fingerprint, " + groupTags
		rateOrderBy := "fingerprint, " + orderBy
		op := "max(value)"
		subQuery := fmt.Sprintf(
			queryTmpl, rateGroupTags, step, op, filterSubQuery, rateGroupBy, rateOrderBy,
		) // labels will be same so any should be fine
		query := `SELECT %s ts, runningDifference(value)/runningDifference(ts) as value FROM(%s) OFFSET 1`
		query = fmt.Sprintf(query, groupTags, subQuery)
		query = fmt.Sprintf(`SELECT %s ts, sum(value) as value FROM (%s) GROUP BY %s ORDER BY %s ts`, groupTags, query, groupBy, orderBy)
		value := aggregateOperatorToPercentile[mq.AggregateOperator]

		query = fmt.Sprintf(`SELECT %s ts, histogramQuantile(arrayMap(x -> toFloat64(x), groupArray(le)), groupArray(value), %.3f) as value FROM (%s) GROUP BY %s ORDER BY %s ts`, groupTagsWithoutLe, value, query, groupByWithoutLe, orderWithoutLe)
		return query, nil
	case v3.AggregateOperatorAvg, v3.AggregateOperatorSum, v3.AggregateOperatorMin, v3.AggregateOperatorMax:
		op := fmt.Sprintf("%s(value)", aggregateOperatorToSQLFunc[mq.AggregateOperator])
		query := fmt.Sprintf(queryTmpl, groupTags, step, op, filterSubQuery, groupBy, orderBy)
		return query, nil
	case v3.AggregateOpeatorCount:
		op := "toFloat64(count(*))"
		query := fmt.Sprintf(queryTmpl, groupTags, step, op, filterSubQuery, groupBy, orderBy)
		return query, nil
	case v3.AggregateOperatorCountDistinct:
		op := "toFloat64(count(distinct(value)))"
		query := fmt.Sprintf(queryTmpl, groupTags, step, op, filterSubQuery, groupBy, orderBy)
		return query, nil
	case v3.AggregateOperatorNoOp:
		queryTmpl :=
			"SELECT fingerprint, labels as fullLabels," +
				" toStartOfInterval(toDateTime(intDiv(timestamp_ms, 1000)), INTERVAL %d SECOND) as ts," +
				" any(value) as value" +
				" FROM " + constants.SIGNOZ_METRIC_DBNAME + "." + constants.SIGNOZ_SAMPLES_TABLENAME +
				" GLOBAL INNER JOIN" +
				" (%s) as filtered_time_series" +
				" USING fingerprint" +
				" WHERE " + samplesTableTimeFilter +
				" GROUP BY fingerprint, labels, ts" +
				" ORDER BY fingerprint, labels, ts"
		query := fmt.Sprintf(queryTmpl, step, filterSubQuery)
		return query, nil
	default:
		return "", fmt.Errorf("unsupported aggregate operator")
	}
}

// groupBy returns a string of comma separated tags for group by clause
// `ts` is always added to the group by clause
func groupBy(tags ...string) string {
	tags = append(tags, "ts")
	return strings.Join(tags, ",")
}

// groupSelect returns a string of comma separated tags for select clause
func groupSelect(tags ...string) string {
	groupTags := strings.Join(tags, ",")
	if len(tags) != 0 {
		groupTags += ", "
	}
	return groupTags
}

// orderBy returns a string of comma separated tags for order by clause
// if the order is not specified, it defaults to ASC
func orderBy(items []v3.OrderBy, tags []string) string {
	var orderBy []string
	for _, tag := range tags {
		found := false
		for _, item := range items {
			if item.ColumnName == tag {
				found = true
				orderBy = append(orderBy, fmt.Sprintf("%s %s", item.ColumnName, item.Order))
				break
			}
		}
		if !found {
			orderBy = append(orderBy, fmt.Sprintf("%s ASC", tag))
		}
	}
	return strings.Join(orderBy, ",")
}

func having(items []v3.Having) string {
	var having []string
	for _, item := range items {
		having = append(having, fmt.Sprintf("%s %s %v", item.ColumnName, item.Operator, utils.ClickHouseFormattedValue(item.Value)))
	}
	return strings.Join(having, " AND ")
}

func reduceQuery(query string, reduceTo v3.ReduceToOperator, aggregateOperator v3.AggregateOperator) (string, error) {
	var selectLabels string
	var groupBy string
	// NOOP and RATE can possibly return multiple time series and reduce should be applied
	// for each uniques series. When the final result contains more than one series we throw
	// an error post DB fetching. Otherwise just return the single data. This is not known until queried so the
	// the query is prepared accordingly.
	if aggregateOperator == v3.AggregateOperatorNoOp || aggregateOperator == v3.AggregateOperatorRate {
		selectLabels = ", any(fullLabels) as fullLabels"
		groupBy = "GROUP BY fingerprint"
	}
	// the timestamp picked is not relevant here since the final value used is show the single
	// chart with just the query value. For the quer
	switch reduceTo {
	case v3.ReduceToOperatorLast:
		query = fmt.Sprintf("SELECT anyLast(value) as value, any(ts) as ts %s FROM (%s) %s", selectLabels, query, groupBy)
	case v3.ReduceToOperatorSum:
		query = fmt.Sprintf("SELECT sum(value) as value, any(ts) as ts %s FROM (%s) %s", selectLabels, query, groupBy)
	case v3.ReduceToOperatorAvg:
		query = fmt.Sprintf("SELECT avg(value) as value, any(ts) as ts %s FROM (%s) %s", selectLabels, query, groupBy)
	case v3.ReduceToOperatorMax:
		query = fmt.Sprintf("SELECT max(value) as value, any(ts) as ts %s FROM (%s) %s", selectLabels, query, groupBy)
	case v3.ReduceToOperatorMin:
		query = fmt.Sprintf("SELECT min(value) as value, any(ts) as ts %s FROM (%s) %s", selectLabels, query, groupBy)
	default:
		return "", fmt.Errorf("unsupported reduce operator")
	}
	return query, nil
}

func PrepareMetricQuery(start, end, step int64, queryType v3.QueryType, panelType v3.PanelType, mq *v3.BuilderQuery) (string, error) {
	query, err := buildMetricQuery(start, end, step, mq, constants.SIGNOZ_TIMESERIES_TABLENAME)
	if err != nil {
		return "", err
	}
	if panelType == v3.PanelTypeValue {
		query, err = reduceQuery(query, mq.ReduceTo, mq.AggregateOperator)
	}
	return query, err
}
