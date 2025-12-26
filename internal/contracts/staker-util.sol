// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface IStaker {
    function firstActive() external view returns (address first);
    function next(address prev) external view returns (address nextValidation);
    function getValidation(address validator) external view returns (
        address endorser,
        uint256 stake,
        uint256 weight,
        uint256 queuedVET,
        uint8 status,
        uint32 offlineBlock
    );
}

/**
 * @title StakerUtil
 * @notice Utility contract for batch querying validators from the Staker contract
 * @dev This is a "ghost contract" - it's not deployed, but used for eth_call with bytecode override
 */
contract StakerUtil {
    // Staker contract address (VeChain built-in)
    address constant STAKER = 0x00000000000000000000000000005374616B6572;

    // Maximum number of validators to return (prevent out of gas)
    uint256 constant MAX_VALIDATORS = 200;

    /**
     * @notice Get all validators (both active and queued)
     * @return validators Array of all validators
     * @return weights Array of all weights
     * @return onlineStatus Array of all online status
     */
    function activeValidators() external view returns (address[] memory validators, uint256[] memory weights, bool[] memory onlineStatus) {
        IStaker staker = IStaker(STAKER);

        // First, count validators
        uint256 activeCount = 0;

        // Count active validators
        address current = staker.firstActive();
        while (current != address(0) && activeCount < MAX_VALIDATORS) {
            activeCount++;
            current = staker.next(current);
        }

        // Allocate array
        validators = new address[](activeCount);
        weights = new uint256[](activeCount);
        onlineStatus = new bool[](activeCount);
        uint256 index = 0;

        // Populate active validators
        current = staker.firstActive();
        while (current != address(0) && index < activeCount) {
            validators[index] = current;

            (
                ,
                ,
                uint256 weight,
                ,
                ,
                uint32 offlineBlock
            ) = staker.getValidation(current);
            weights[index] = weight;
            onlineStatus[index] = offlineBlock == type(uint32).max ? true : false;

            index++;
            current = staker.next(current);
        }
        return (validators, weights, onlineStatus);
    }
}
