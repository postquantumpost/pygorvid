from pygorvid.h264_deferred import (
    _NUMPY_BATCH_MIN_SAMPLES,
    _combine_prediction_batch,
    _combine_predictions,
)
from pygorvid.h264_weighted_prediction import (
    PredictionWeight,
    SampleWeights,
)


def test_multiple_partition_predictions_batch_matches_scalar_combine():
    size = (_NUMPY_BATCH_MIN_SAMPLES + 2) // 3
    first = tuple([list(range(component, component + size)) for component in range(3)])
    second = tuple(
        [list(range(component + 10, component + 10 + size)) for component in range(3)]
    )
    weights = SampleWeights(
        (3, 2, 4),
        (
            PredictionWeight((5, 6, 7), (1, -2, 3)),
            PredictionWeight((9, 10, 11), (-1, 2, -3)),
        ),
    )
    batches = [
        ((first, second), weights),
        ((first, None), None),
        ((None, second), weights),
    ]

    expected = [
        _combine_predictions(predictions, selected_weights)
        for predictions, selected_weights in batches
    ]

    assert _combine_prediction_batch(batches) == expected


def test_single_partition_prediction_batch_uses_scalar_semantics():
    predictions = (((10, 20), (30,), (40,)), ((14, 24), (34,), (44,)))

    assert _combine_prediction_batch([(predictions, None)]) == [
        _combine_predictions(predictions, None)
    ]
